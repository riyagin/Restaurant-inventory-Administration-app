package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
)

// Automatic payroll → ledger posting.
//
// Closing a period queues a payroll_postings row inside the close transaction;
// a background worker then writes ONE balanced journal entry for the period and
// marks the row posted. The close request itself returns as soon as the payroll
// data is committed, so the operator is never left waiting on the ledger.
//
// The queue row is what makes "background" safe: it is committed with the close,
// so a process that dies mid-post leaves durable evidence that the entry is still
// owed, and SweepUnposted picks it up on the next start (and every tick after).

// CashAccountNumber is the system asset account ("Kas") payroll is paid out of.
const CashAccountNumber = 11000

// MaxPostingAttempts caps retries for one period. A posting that fails this many
// times is stuck on something a retry cannot fix (a deleted account, a broken
// migration), so it stops consuming sweep cycles and waits for a human — the
// period shows as failed in the UI with the error text.
const MaxPostingAttempts = 5

// PostingSweepInterval is how often the worker rechecks for unposted periods.
const PostingSweepInterval = 5 * time.Minute

// WageAccountName returns the per-branch payroll expense account name created by
// migration 037 ("Beban Gaji - <cabang>").
func WageAccountName(branch string) string {
	return "Beban Gaji - " + branch
}

// PayrollEntryDescription is the journal description for a period's posting.
func PayrollEntryDescription(periodMonth time.Time) string {
	return fmt.Sprintf("Gaji %s", periodMonth.Format("2006-01"))
}

// QueuePayrollPosting records that a period owes a ledger entry. Call inside the
// close transaction so the queue row and the closed period commit together.
func QueuePayrollPosting(ctx context.Context, qtx *db.Queries, periodID pgtype.UUID) error {
	return qtx.UpsertPayrollPosting(ctx, periodID)
}

// branchExpenseAmount is the wage expense debited for one branch.
//
// The expense debited is net + kasbon, NOT gross. The difference between gross and
// that figure is money the employee never receives and the company never pays out:
//
//	unpaid leave / half-day  → work not performed, so never an expense
//	component deductions     → withheld from the employee (BPJS and friends)
//	kasbon                   → not an expense here; it clears the employee
//	                           receivable that the advance created, so it gets its
//	                           own credit leg against Piutang Karyawan
//
// so: Dr Beban Gaji (net + kasbon), Cr Kas (net), Cr Piutang Karyawan (kasbon).
func branchExpenseAmount(net, kasbon int64) int64 { return net + kasbon }

// resolveWageAccount finds the account to debit for a branch: its dedicated
// "Beban Gaji - <cabang>" account, falling back to the branch's generic expense
// account. Returns uuid.Nil when neither exists — Post then parks the leg in the
// suspense account rather than dropping it, so the entry still balances and the
// misconfiguration is visible in the CoA.
func resolveWageAccount(ctx context.Context, qtx *db.Queries, branchID pgtype.UUID, branchName string) uuid.UUID {
	if branchName != "" {
		if acct, err := qtx.GetAccountByName(ctx, WageAccountName(branchName)); err == nil && acct != nil {
			return acct.ID.Bytes
		}
	}
	if branchID.Valid {
		if expense, err := qtx.GetBranchExpenseAccountID(ctx, branchID); err == nil && expense.Valid {
			return expense.Bytes
		}
	}
	return uuid.Nil
}

// AlreadyPosted reports whether the journal already carries an entry for this
// payroll period. Belt-and-braces alongside the payroll_postings PK: it makes a
// double post impossible even if the queue row were lost or reset.
func AlreadyPosted(ctx context.Context, qtx *db.Queries, periodID pgtype.UUID) (bool, error) {
	entries, err := qtx.ListJournalEntriesBySource(ctx, &db.ListJournalEntriesBySourceParams{
		SourceType: SourcePayroll,
		SourceID:   periodID,
	})
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

// PostPayrollPeriod writes the period's single balanced journal entry. Idempotent:
// a period that already has a 'payroll' entry is marked posted and left alone.
// Must run inside a transaction.
func PostPayrollPeriod(ctx context.Context, qtx *db.Queries, period *db.PayrollPeriod, actor uuid.UUID) (uuid.UUID, error) {
	posted, err := AlreadyPosted(ctx, qtx, period.ID)
	if err != nil {
		return uuid.Nil, err
	}
	if posted {
		return uuid.Nil, nil
	}

	totals, err := qtx.ListPayrollBranchPostingTotals(ctx, period.ID)
	if err != nil {
		return uuid.Nil, err
	}

	var (
		lines       []Line
		totalNet    int64
		totalKasbon int64
	)
	for _, t := range totals {
		branchName := ""
		if t.BranchName.Valid {
			branchName = t.BranchName.String
		}
		amount := branchExpenseAmount(t.TotalNet, t.TotalKasbon)
		if amount == 0 {
			continue
		}
		acct := resolveWageAccount(ctx, qtx, t.BranchID, branchName)
		memo := branchName
		if memo == "" {
			memo = "Tanpa Cabang"
		}
		lines = append(lines, Dr(acct, amount).WithMemo(memo))
		totalNet += t.TotalNet
		totalKasbon += t.TotalKasbon
	}

	if len(lines) == 0 {
		// Nothing to pay (an empty period). Not an error — no entry.
		return uuid.Nil, nil
	}

	cash, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: CashAccountNumber, Valid: true})
	if err != nil {
		return uuid.Nil, fmt.Errorf("akun sistem 'Kas' (%d) tidak ditemukan: %w", CashAccountNumber, err)
	}
	lines = append(lines, Cr(cash.ID.Bytes, totalNet).WithMemo("pembayaran gaji"))

	if totalKasbon != 0 {
		piutang, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: PiutangKaryawanAccountNumber, Valid: true})
		if err != nil {
			return uuid.Nil, ErrPiutangAccountMissing
		}
		lines = append(lines, Cr(piutang.ID.Bytes, totalKasbon).WithMemo("pelunasan kasbon dari gaji"))
	}

	return Post(ctx, qtx, Entry{
		Date:        period.EndDate.Time,
		SourceType:  SourcePayroll,
		SourceID:    period.ID.Bytes,
		Description: PayrollEntryDescription(period.PeriodMonth.Time),
		CreatedBy:   actor,
		Lines:       lines,
	})
}

// ── Background worker ────────────────────────────────────────────────────────

// PayrollPoster runs payroll postings off the request path. Enqueue fires one
// immediately after a close commits; SweepUnposted catches anything that failed
// or was interrupted, which is what makes the async path safe to rely on.
type PayrollPoster struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewPayrollPoster(pool *pgxpool.Pool, queries *db.Queries) *PayrollPoster {
	return &PayrollPoster{pool: pool, queries: queries}
}

// Enqueue posts a period in the background. The caller's request context is
// deliberately NOT used: it is cancelled the moment the HTTP response is written,
// which would kill the posting we just promised to do.
func (p *PayrollPoster) Enqueue(periodID pgtype.UUID, actor uuid.UUID) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := p.PostOne(ctx, periodID, actor); err != nil {
			log.Printf("payroll posting %s gagal: %v", uuid.UUID(periodID.Bytes), err)
		}
	}()
}

// PostOne runs one period's posting in its own transaction and records the
// outcome on the queue row. A failure is recorded and returned, never swallowed:
// the row stays unposted so the sweep retries it.
func (p *PayrollPoster) PostOne(ctx context.Context, periodID pgtype.UUID, actor uuid.UUID) error {
	// The attempt counter is bumped in its own transaction so it survives the
	// rollback of a failed posting — otherwise a permanently broken period would
	// retry forever.
	if err := p.queries.MarkPayrollPostingAttempt(ctx, periodID); err != nil {
		return err
	}

	err := p.post(ctx, periodID, actor)
	if err != nil {
		msg := err.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		if ferr := p.queries.MarkPayrollPostingFailed(ctx, &db.MarkPayrollPostingFailedParams{
			PeriodID:  periodID,
			LastError: msg,
		}); ferr != nil {
			return fmt.Errorf("%w (dan gagal mencatat kegagalan: %v)", err, ferr)
		}
		return err
	}
	return nil
}

func (p *PayrollPoster) post(ctx context.Context, periodID pgtype.UUID, actor uuid.UUID) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := p.queries.WithTx(tx)

	period, err := qtx.GetPayrollPeriodByID(ctx, periodID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Period deleted before the posting ran — nothing owed.
			return nil
		}
		return err
	}

	entryID, err := PostPayrollPeriod(ctx, qtx, period, actor)
	if err != nil {
		return err
	}

	if err := qtx.MarkPayrollPostingPosted(ctx, &db.MarkPayrollPostingPostedParams{
		PeriodID:       periodID,
		JournalEntryID: pgtype.UUID{Bytes: entryID, Valid: entryID != uuid.Nil},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SweepUnposted retries every period that is queued or failed and still under the
// attempt cap. Runs once at startup (catching anything a restart interrupted) and
// then on every tick.
func (p *PayrollPoster) SweepUnposted(ctx context.Context) {
	rows, err := p.queries.ListUnpostedPayrollPostings(ctx, MaxPostingAttempts)
	if err != nil {
		log.Printf("payroll posting sweep: %v", err)
		return
	}
	for _, r := range rows {
		if err := p.PostOne(ctx, r.PeriodID, uuid.Nil); err != nil {
			log.Printf("payroll posting %s gagal (percobaan %d): %v",
				uuid.UUID(r.PeriodID.Bytes), r.Attempts+1, err)
		}
	}
}

// Start runs the sweep immediately and then on PostingSweepInterval until ctx is
// cancelled. Call once from main.
func (p *PayrollPoster) Start(ctx context.Context) {
	go func() {
		p.SweepUnposted(ctx)
		ticker := time.NewTicker(PostingSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.SweepUnposted(ctx)
			}
		}
	}()
}
