package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Beban Operasional over time, per branch.
//
// Deliberately read from `operational_expenses` rather than from the journal,
// which is the opposite of what every other report here does — and for a reason
// that only applies to this one. The journal is keyed on `entry_date`, the day
// the money moved. This report is about `period_month`, the month the bill
// covers, and those differ by however long it took to pay: July's electricity
// settled in August is a July figure here and an August figure in the P&L. Both
// are right for their own question. Asking "did the water bill jump in June" of
// the ledger would answer with when invoices happened to be paid, which is noise
// dressed as a trend.
//
// So this is a usage report, not a restatement of the P&L, and it says so on the
// page. Cancelled rows are excluded because they were reversed.

// recurringOperationalCategories are the bills that arrive every month, whether
// or not anyone did anything. They each get their own line, because a line that
// is supposed to appear monthly makes a *missing* month meaningful — the gap is
// the finding.
//
// Everything else — Perbaikan, Lain-lain, and whatever a branch has added — is
// ad-hoc by nature: a repair in March says nothing about April. Charting those
// individually would produce a page of near-empty rows and bury the four lines
// that carry information, so they collapse into a single "Lainnya" row. Matched
// case-insensitively on the name, since a hand-added category is free text, and
// by name rather than by id because each branch owns its own copy of "Listrik".
var recurringOperationalCategories = []string{
	"Listrik", "Air", "Gas", "Internet", "Telepon", "Sewa", "Kebersihan", "Keamanan",
}

// otherOperationalCategory is the row everything non-recurring folds into.
const otherOperationalCategory = "Lainnya"

// utilityOperationalCategories are the metered ones — the bills that move with
// how much a branch actually consumed rather than with a contract. They are the
// reason this page exists, so they are charted; the rest are table rows.
var utilityOperationalCategories = []string{"Listrik", "Air", "Gas"}

const maxOperationalMonths = 36

// OperationalExpenses — GET /api/reports/operational-expenses
// Params: months (1..36, default 12), end (YYYY-MM, default this month),
//
//	branch_id (optional).
func (h *ReportsHandler) OperationalExpenses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ctx := r.Context()

	months := 12
	if v := q.Get("months"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxOperationalMonths {
			respondError(w, http.StatusBadRequest,
				"parameter 'months' harus antara 1 dan "+strconv.Itoa(maxOperationalMonths))
			return
		}
		months = n
	}

	end := time.Now().UTC()
	if v := q.Get("end"); v != "" {
		t, err := time.Parse("2006-01", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "parameter 'end' harus YYYY-MM")
			return
		}
		end = t
	}
	endMonth := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)
	startMonth := endMonth.AddDate(0, -(months - 1), 0)

	type monthCol struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	cols := make([]monthCol, 0, months)
	for i := 0; i < months; i++ {
		m := startMonth.AddDate(0, i, 0)
		cols = append(cols, monthCol{
			Key:   m.Format("2006-01"),
			Label: monthAbbrevID[int(m.Month())-1] + " " + m.Format("2006"),
		})
	}

	branchFilter := q.Get("branch_id")
	if branchFilter != "" {
		if _, err := parseUUID(branchFilter); err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
	}

	sql := `
		SELECT b.id::text AS branch_id, b.name AS branch_name,
		       oec.name AS category,
		       to_char(oe.period_month, 'YYYY-MM') AS ym,
		       SUM(oe.amount)::BIGINT AS amount,
		       COUNT(*)::INT AS entries
		FROM operational_expenses oe
		JOIN branches b ON b.id = oe.branch_id
		JOIN operational_expense_categories oec ON oec.id = oe.category_id
		WHERE oe.status = 'posted' AND oe.period_month BETWEEN $1 AND $2`
	params := []any{
		pgtype.Date{Time: startMonth, Valid: true},
		pgtype.Date{Time: endMonth, Valid: true},
	}
	if branchFilter != "" {
		sql += ` AND oe.branch_id = $3`
		params = append(params, branchFilter)
	}
	sql += ` GROUP BY b.id, b.name, oec.name, ym`

	rows, err := h.pool.Query(ctx, sql, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil beban operasional")
		return
	}
	defer rows.Close()

	recurring := map[string]string{} // lower-case name → canonical name
	for _, name := range recurringOperationalCategories {
		recurring[strings.ToLower(name)] = name
	}

	type cell struct {
		amount  int64
		entries int
	}
	// branch id → category name → month key
	data := map[string]map[string]map[string]cell{}
	branchNames := map[string]string{}

	for rows.Next() {
		var branchID, branchName, category, ym string
		var amount int64
		var entries int
		if err := rows.Scan(&branchID, &branchName, &category, &ym, &amount, &entries); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses beban operasional")
			return
		}
		branchNames[branchID] = branchName

		label := otherOperationalCategory
		if canonical, ok := recurring[strings.ToLower(category)]; ok {
			label = canonical
		}
		if data[branchID] == nil {
			data[branchID] = map[string]map[string]cell{}
		}
		if data[branchID][label] == nil {
			data[branchID][label] = map[string]cell{}
		}
		prev := data[branchID][label][ym]
		data[branchID][label][ym] = cell{amount: prev.amount + amount, entries: prev.entries + entries}
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses beban operasional")
		return
	}

	// Every branch is listed even with nothing recorded, and every recurring
	// category is listed even where it has never been paid. A branch or a bill
	// that is simply absent from the page reads as "nothing to see"; one showing
	// a row of zeros reads as "nobody has recorded this", which is the finding.
	bRows, err := h.pool.Query(ctx, `SELECT id::text, name FROM branches ORDER BY name`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data cabang")
		return
	}
	defer bRows.Close()

	type categoryRow struct {
		Category  string           `json:"category"`
		Recurring bool             `json:"recurring"`
		Utility   bool             `json:"utility"`
		Amounts   map[string]int64 `json:"amounts"`
		Entries   map[string]int   `json:"entries"`
		Total     int64            `json:"total"`
		Average   int64            `json:"average"`
		// MonthsRecorded counts the months in the window that carry a figure —
		// the denominator behind Average, and on a recurring row the direct
		// measure of how complete the record is.
		MonthsRecorded int `json:"months_recorded"`
		// ChangePct compares the most recent recorded month with the one before
		// it. Null when either is missing: "no change" and "no data" are
		// different answers and must not look alike.
		ChangePct *float64 `json:"change_pct"`
		Latest    int64    `json:"latest"`
		Previous  int64    `json:"previous"`
	}
	type branchBlock struct {
		ID           string           `json:"id"`
		Name         string           `json:"name"`
		Categories   []categoryRow    `json:"categories"`
		TotalByMonth map[string]int64 `json:"total_by_month"`
		Total        int64            `json:"total"`
	}

	utility := map[string]bool{}
	for _, name := range utilityOperationalCategories {
		utility[name] = true
	}

	// Recurring rows first in their declared order, then Lainnya last — it is the
	// residual, and a residual belongs at the bottom of a column of figures.
	order := append(append([]string{}, recurringOperationalCategories...), otherOperationalCategory)

	branches := []branchBlock{}
	for bRows.Next() {
		var id, name string
		if err := bRows.Scan(&id, &name); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses data cabang")
			return
		}
		if branchFilter != "" && id != branchFilter {
			continue
		}
		block := branchBlock{ID: id, Name: name, TotalByMonth: map[string]int64{}}
		for _, col := range cols {
			block.TotalByMonth[col.Key] = 0
		}

		for _, category := range order {
			cells := data[id][category]
			row := categoryRow{
				Category:  category,
				Recurring: category != otherOperationalCategory,
				Utility:   utility[category],
				Amounts:   map[string]int64{},
				Entries:   map[string]int{},
			}
			var lastKey, prevKey string
			for _, col := range cols {
				c := cells[col.Key]
				row.Amounts[col.Key] = c.amount
				row.Entries[col.Key] = c.entries
				row.Total += c.amount
				block.TotalByMonth[col.Key] += c.amount
				if c.entries > 0 {
					prevKey, lastKey = lastKey, col.Key
					row.MonthsRecorded++
				}
			}
			if row.MonthsRecorded > 0 {
				row.Average = row.Total / int64(row.MonthsRecorded)
			}
			if lastKey != "" {
				row.Latest = row.Amounts[lastKey]
			}
			if prevKey != "" && lastKey != "" {
				row.Previous = row.Amounts[prevKey]
				if row.Previous != 0 {
					pct := (float64(row.Latest-row.Previous) / float64(row.Previous)) * 100
					row.ChangePct = &pct
				}
			}
			block.Total += row.Total
			block.Categories = append(block.Categories, row)
		}
		branches = append(branches, block)
	}
	if err := bRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data cabang")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"months":     cols,
		"start":      startMonth.Format("2006-01"),
		"end":        endMonth.Format("2006-01"),
		"branches":   branches,
		"recurring":  recurringOperationalCategories,
		"utilities":  utilityOperationalCategories,
		"other_name": otherOperationalCategory,
	})
}
