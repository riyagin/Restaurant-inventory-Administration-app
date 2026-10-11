package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
)

// Sewa tahunan: an annual rent payment is booked to the branch's
// "Sewa Dibayar di Muka" asset and expensed one month at a time. The payment
// handler writes the schedule (operational_expense_amortizations); this file
// turns due schedule rows into journal entries.
//
// Each month is posted only once that month has started. Writing all twelve on
// the day of payment would date eleven entries in the future, and since
// accounts.balance moves the moment an entry is posted, the balance sheet would
// show next year's rent as already used up.

// AnnualTermMonths is how many months an annual payment is spread over.
const AnnualTermMonths = 12

// MaxAmortizationAttempts caps retries of a row that keeps failing, so one
// broken row cannot be retried forever. Same idea as MaxPostingAttempts.
const MaxAmortizationAttempts = 5

// AmortizationSweepInterval is how often the poster looks for months that have
// started. Hourly is plenty: a row only becomes due once a month.
const AmortizationSweepInterval = time.Hour

// AmortizationSlice is one month of a schedule.
type AmortizationSlice struct {
	Seq         int
	PeriodMonth time.Time // first day of the month
	Amount      int64
}

// AmortizationSchedule splits total into `months` equal monthly slices starting
// at start's month. Integer division leaves a remainder; it goes on the last
// slice, so the slices always sum to exactly what was paid and the prepaid
// account ends the lease at zero rather than at a few rupiah of rounding.
func AmortizationSchedule(total int64, start time.Time, months int) []AmortizationSlice {
	if months < 1 || total <= 0 {
		return nil
	}
	first := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	each := total / int64(months)
	out := make([]AmortizationSlice, months)
	for i := 0; i < months; i++ {
		amount := each
		if i == months-1 {
			amount = total - each*int64(months-1)
		}
		out[i] = AmortizationSlice{Seq: i + 1, PeriodMonth: first.AddDate(0, i, 0), Amount: amount}
	}
	return out
}

// amortizationEntryDate is the journal date for one month's slice: the first of
// that month, unless the rent was paid later than that. A lease recorded in
// March whose term began in January catches January and February up on the
// payment date rather than reaching back into months already reported on — the
// same rule cancellations follow.
func amortizationEntryDate(periodMonth, paidOn time.Time) time.Time {
	if paidOn.After(periodMonth) {
		return paidOn
	}
	return periodMonth
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// PostAmortizationRow posts one scheduled month, inside the caller's
// transaction. It locks the row and re-checks it under the lock, so the
// background sweep, the payment handler's catch-up and a concurrent
// cancellation can all reach the same row without posting it twice or posting
// it after the lease was cancelled. Returns false (and no error) when there was
// nothing to do: already posted, cancelled, or not due yet.
func PostAmortizationRow(ctx context.Context, tx pgx.Tx, qtx *db.Queries, rowID uuid.UUID, asOf time.Time, actor uuid.UUID) (bool, error) {
	var (
		expenseID                  uuid.UUID
		seq                        int
		periodMonth, paidOn        time.Time
		amount                     int64
		expenseAcct, prepaidAcct   uuid.UUID
		status, expenseStatus      string
		number, branchName         string
	)
	err := tx.QueryRow(ctx, `
		SELECT a.expense_id, a.seq, a.period_month, a.amount, a.expense_account_id, a.prepaid_account_id,
		       a.status, oe.status, oe.date, oe.number, b.name
		FROM operational_expense_amortizations a
		JOIN operational_expenses oe ON oe.id = a.expense_id
		JOIN branches b ON b.id = oe.branch_id
		WHERE a.id = $1
		FOR UPDATE OF a`, rowID).
		Scan(&expenseID, &seq, &periodMonth, &amount, &expenseAcct, &prepaidAcct,
			&status, &expenseStatus, &paidOn, &number, &branchName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if status != "scheduled" || expenseStatus != "posted" || periodMonth.After(monthStart(asOf)) {
		return false, nil
	}

	entryID, err := Post(ctx, qtx, Entry{
		Date:       amortizationEntryDate(periodMonth, paidOn),
		SourceType: SourceOperationalExpense,
		SourceID:   expenseID,
		Description: fmt.Sprintf("Amortisasi sewa %s: %s - bulan %d/%d (%s %d)",
			number, branchName, seq, AnnualTermMonths, indoMonths[periodMonth.Month()], periodMonth.Year()),
		CreatedBy: actor,
		Lines: []Line{
			Dr(expenseAcct, amount),
			Cr(prepaidAcct, amount),
		},
	})
	if err != nil {
		return false, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE operational_expense_amortizations
		SET status = 'posted', journal_entry_id = $1, posted_at = now(), last_error = ''
		WHERE id = $2`, entryID, rowID); err != nil {
		return false, err
	}
	return true, nil
}

// PostDueAmortizationsFor posts every month of one lease that is already due,
// inside the caller's transaction. The payment handler calls it right after
// writing the schedule, so a lease whose term has already begun shows its
// elapsed months as expense immediately instead of waiting for the next sweep.
func PostDueAmortizationsFor(ctx context.Context, tx pgx.Tx, qtx *db.Queries, expenseID uuid.UUID, asOf time.Time, actor uuid.UUID) (int, error) {
	rows, err := tx.Query(ctx, `
		SELECT id FROM operational_expense_amortizations
		WHERE expense_id = $1 AND status = 'scheduled' AND period_month <= $2
		ORDER BY seq`, expenseID, monthStart(asOf))
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, err
	}
	posted := 0
	for _, id := range ids {
		ok, err := PostAmortizationRow(ctx, tx, qtx, id, asOf, actor)
		if err != nil {
			return posted, err
		}
		if ok {
			posted++
		}
	}
	return posted, nil
}

// ── Background worker ────────────────────────────────────────────────────────

// AmortizationPoster posts scheduled months as they come due. It runs at
// startup — catching up anything a stopped server missed — and then hourly.
type AmortizationPoster struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewAmortizationPoster(pool *pgxpool.Pool, queries *db.Queries) *AmortizationPoster {
	return &AmortizationPoster{pool: pool, queries: queries}
}

// Sweep posts every due row, one transaction per row so one failure does not
// hold back the rest. Returns how many rows were posted.
func (p *AmortizationPoster) Sweep(ctx context.Context, asOf time.Time) int {
	rows, err := p.pool.Query(ctx, `
		SELECT a.id FROM operational_expense_amortizations a
		JOIN operational_expenses oe ON oe.id = a.expense_id
		WHERE a.status = 'scheduled' AND oe.status = 'posted'
		  AND a.period_month <= $1 AND a.attempts < $2
		ORDER BY a.period_month, a.seq`, monthStart(asOf), MaxAmortizationAttempts)
	if err != nil {
		log.Printf("amortisasi sewa: gagal membaca jadwal: %v", err)
		return 0
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		log.Printf("amortisasi sewa: gagal membaca jadwal: %v", err)
		return 0
	}

	posted := 0
	for _, id := range ids {
		ok, err := p.postOne(ctx, id, asOf)
		if err != nil {
			msg := err.Error()
			if len(msg) > 500 {
				msg = msg[:500]
			}
			// Recorded outside the failed transaction so the attempt counter
			// survives its rollback.
			if _, ferr := p.pool.Exec(ctx, `
				UPDATE operational_expense_amortizations
				SET attempts = attempts + 1, last_error = $1 WHERE id = $2`, msg, id); ferr != nil {
				log.Printf("amortisasi sewa %s: gagal mencatat kegagalan: %v", id, ferr)
			}
			log.Printf("amortisasi sewa %s gagal: %v", id, err)
			continue
		}
		if ok {
			posted++
		}
	}
	return posted
}

func (p *AmortizationPoster) postOne(ctx context.Context, id uuid.UUID, asOf time.Time) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	ok, err := PostAmortizationRow(ctx, tx, p.queries.WithTx(tx), id, asOf, uuid.Nil)
	if err != nil {
		return false, err
	}
	return ok, tx.Commit(ctx)
}

// Start runs a sweep immediately and then every AmortizationSweepInterval until
// ctx is cancelled. Call once from main.
func (p *AmortizationPoster) Start(ctx context.Context) {
	go func() {
		p.Sweep(ctx, time.Now())
		ticker := time.NewTicker(AmortizationSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				p.Sweep(ctx, t)
			}
		}
	}()
}
