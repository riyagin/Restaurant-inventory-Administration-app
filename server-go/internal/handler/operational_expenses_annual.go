package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/service"
)

// ensurePrepaidRentAccount numbers within 10501-10599, outside the 11000-19999 MAX+1
// range used for inventory accounts. The branch row is locked against duplicates.
func ensurePrepaidRentAccount(ctx context.Context, tx pgx.Tx, branchID pgtype.UUID, branchName string) (pgtype.UUID, error) {
	var existing pgtype.UUID
	if err := tx.QueryRow(ctx,
		`SELECT prepaid_rent_account_id FROM branches WHERE id = $1 FOR UPDATE`, branchID).Scan(&existing); err != nil {
		return pgtype.UUID{}, err
	}
	if existing.Valid {
		return existing, nil
	}

	var id pgtype.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO accounts (name, account_number, account_type, parent_id)
		SELECT $1, n.next, 'asset', p.id
		FROM (SELECT COALESCE(MAX(account_number), 10500) + 1 AS next
		      FROM accounts WHERE account_number BETWEEN 10501 AND 10599) n,
		     (SELECT id FROM accounts WHERE account_number = 10500) p
		WHERE n.next <= 10599
		RETURNING id`, "Sewa Dibayar di Muka - "+branchName).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, errors.New("nomor akun sewa dibayar di muka (10501-10599) sudah habis")
		}
		return pgtype.UUID{}, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE branches SET prepaid_rent_account_id = $1 WHERE id = $2`, id, branchID); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

// ListAnnual — GET /api/operational-expenses/annual?branch_id=&include_cancelled=
// "Renewed": a later annual payment exists for the same branch and category.
func (h *OperationalExpensesHandler) ListAnnual(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := []string{"oe.billing = 'annual'"}
	var params []any
	if v := q.Get("branch_id"); v != "" {
		id, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		params = append(params, pgUUID(id))
		where = append(where, fmt.Sprintf("oe.branch_id = $%d", len(params)))
	}
	if q.Get("include_cancelled") != "true" {
		where = append(where, "oe.status = 'posted'")
	}

	ctx := r.Context()
	rows, err := h.pool.Query(ctx, operationalExpenseSelect+` WHERE `+strings.Join(where, " AND ")+
		` ORDER BY oe.due_date DESC, oe.created_at DESC`, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data sewa tahunan")
		return
	}
	leases, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data sewa tahunan")
		return
	}

	ids := make([]string, 0, len(leases))
	for _, l := range leases {
		ids = append(ids, l["id"].(string))
	}

	type slice struct {
		Seq         int        `json:"seq"`
		PeriodMonth string     `json:"period_month"`
		Amount      int64      `json:"amount"`
		Status      string     `json:"status"`
		PostedAt    *time.Time `json:"posted_at"`
		LastError   string     `json:"last_error"`
	}
	schedules := map[string][]slice{}
	if len(ids) > 0 {
		sRows, err := h.pool.Query(ctx, `
			SELECT expense_id::text, seq, to_char(period_month, 'YYYY-MM-DD'), amount, status, posted_at, last_error
			FROM operational_expense_amortizations
			WHERE expense_id = ANY($1::uuid[])
			ORDER BY expense_id, seq`, ids)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil jadwal amortisasi")
			return
		}
		for sRows.Next() {
			var expenseID string
			var s slice
			if err := sRows.Scan(&expenseID, &s.Seq, &s.PeriodMonth, &s.Amount, &s.Status, &s.PostedAt, &s.LastError); err != nil {
				sRows.Close()
				respondError(w, http.StatusInternalServerError, "gagal memproses jadwal amortisasi")
				return
			}
			schedules[expenseID] = append(schedules[expenseID], s)
		}
		sRows.Close()
		if sRows.Err() != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses jadwal amortisasi")
			return
		}
	}

	type key struct{ branch, category string }
	latestDue := map[key]time.Time{}
	for _, l := range leases {
		if l["status"] != "posted" {
			continue
		}
		due, _ := l["due_date"].(time.Time)
		k := key{l["branch_id"].(string), l["category_id"].(string)}
		if due.After(latestDue[k]) {
			latestDue[k] = due
		}
	}

	out := make([]map[string]any, 0, len(leases))
	for _, l := range leases {
		id := l["id"].(string)
		sched := schedules[id]
		if sched == nil {
			sched = []slice{}
		}
		var amortized int64
		posted := 0
		for _, s := range sched {
			if s.Status == "posted" {
				amortized += s.Amount
				posted++
			}
		}
		due, _ := l["due_date"].(time.Time)
		start := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.UTC)
		nextDue := start.AddDate(0, service.AnnualTermMonths, 0)
		k := key{l["branch_id"].(string), l["category_id"].(string)}

		l["schedule"] = sched
		l["amortized"] = amortized
		l["months_posted"] = posted
		l["remaining"] = toInt64(l["amount"]) - amortized
		l["coverage_start"] = start.Format("2006-01-02")
		l["coverage_end"] = nextDue.AddDate(0, 0, -1).Format("2006-01-02")
		l["next_due_date"] = nextDue.Format("2006-01-02")
		l["renewed"] = latestDue[k].After(start)
		out = append(out, l)
	}

	respondJSON(w, http.StatusOK, out)
}
