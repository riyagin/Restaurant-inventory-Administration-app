package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// PerformanceHandler serves the JWT (admin/manager) performance scoring endpoints.
type PerformanceHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewPerformanceHandler(pool *pgxpool.Pool, queries *db.Queries) *PerformanceHandler {
	return &PerformanceHandler{pool: pool, queries: queries}
}

func validRuleType(t string) bool {
	switch t {
	case "late", "early_leave", "missing_checkout", "missing_checkin", "no_punch", "absent_no_leave", "consecutive_absent", "half_day_late", "half_day_early", "manual":
		return true
	}
	return false
}

// firstOfMonthDate parses a "YYYY-MM" (or "YYYY-MM-DD") string into the first day
// of that month. Empty defaults to the current month.
func firstOfMonthDate(monthStr string) (time.Time, error) {
	monthStr = strings.TrimSpace(monthStr)
	if monthStr == "" {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), nil
	}
	if len(monthStr) == 7 {
		monthStr += "-01"
	}
	t, err := time.Parse("2006-01-02", monthStr)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC), nil
}

// ── Policies ─────────────────────────────────────────────────────────────────

// ListPolicies — GET /api/hr/performance/policies
func (h *PerformanceHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.queries.ListPerformancePolicies(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil kebijakan kinerja")
		return
	}
	if rows == nil {
		rows = []*db.PerformancePolicy{}
	}
	respondJSON(w, http.StatusOK, rows)
}

type policyBody struct {
	Name                   string `json:"name"`
	RuleType               string `json:"rule_type"`
	ThresholdMinutes       *int32 `json:"threshold_minutes"`
	Points                 int32  `json:"points"`
	MaxOccurrencesPerMonth *int32 `json:"max_occurrences_per_month"`
	IsActive               *bool  `json:"is_active"`
}

func (b *policyBody) validate() (string, bool) {
	b.Name = strings.TrimSpace(b.Name)
	b.RuleType = strings.TrimSpace(b.RuleType)
	if b.Name == "" {
		return "nama kebijakan wajib diisi", false
	}
	if !validRuleType(b.RuleType) {
		return "tipe aturan tidak valid", false
	}
	if b.Points <= 0 {
		return "poin pengurangan harus lebih dari 0", false
	}
	// Threshold only meaningful for late / early_leave (minutes) and
	// consecutive_absent (days). Cleared for every other rule.
	if b.RuleType != "late" && b.RuleType != "early_leave" && b.RuleType != "consecutive_absent" {
		b.ThresholdMinutes = nil
	}
	return "", true
}

func int4Ptr(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

// CreatePolicy — POST /api/hr/performance/policies
func (h *PerformanceHandler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	var body policyBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if msg, ok := body.validate(); !ok {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	ctx := r.Context()
	pol, err := h.queries.CreatePerformancePolicy(ctx, &db.CreatePerformancePolicyParams{
		Name:                   body.Name,
		RuleType:               body.RuleType,
		ThresholdMinutes:       int4Ptr(body.ThresholdMinutes),
		Points:                 body.Points,
		MaxOccurrencesPerMonth: int4Ptr(body.MaxOccurrencesPerMonth),
		IsActive:               isActive,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat kebijakan kinerja")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "performance_policy",
		EntityID:    pol.ID.Bytes,
		Description: fmt.Sprintf("Membuat kebijakan kinerja %s", pol.Name),
	})

	respondJSON(w, http.StatusCreated, pol)
}

// UpdatePolicy — PUT /api/hr/performance/policies/:id
func (h *PerformanceHandler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body policyBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if msg, ok := body.validate(); !ok {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	ctx := r.Context()
	pol, err := h.queries.UpdatePerformancePolicy(ctx, &db.UpdatePerformancePolicyParams{
		Name:                   body.Name,
		RuleType:               body.RuleType,
		ThresholdMinutes:       int4Ptr(body.ThresholdMinutes),
		Points:                 body.Points,
		MaxOccurrencesPerMonth: int4Ptr(body.MaxOccurrencesPerMonth),
		IsActive:               isActive,
		ID:                     pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kebijakan kinerja tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui kebijakan kinerja")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "performance_policy",
		EntityID:    id,
		Description: fmt.Sprintf("Memperbarui kebijakan kinerja %s", pol.Name),
	})

	respondJSON(w, http.StatusOK, pol)
}

// DeletePolicy — DELETE /api/hr/performance/policies/:id
// Deletes only when unreferenced by violations; otherwise deactivates.
func (h *PerformanceHandler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	pol, err := h.queries.GetPerformancePolicyByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kebijakan kinerja tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kebijakan kinerja")
		return
	}

	refs, err := h.queries.CountPolicyViolations(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa penggunaan kebijakan")
		return
	}

	if refs > 0 {
		updated, err := h.queries.SetPerformancePolicyActive(ctx, &db.SetPerformancePolicyActiveParams{
			IsActive: false,
			ID:       pgID,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menonaktifkan kebijakan kinerja")
			return
		}
		_ = service.LogActivity(ctx, h.queries, service.LogParams{
			UserID:      middleware.UserIDFromCtx(ctx),
			Username:    middleware.UsernameFromCtx(ctx),
			Action:      "UPDATE",
			EntityType:  "performance_policy",
			EntityID:    id,
			Description: fmt.Sprintf("Menonaktifkan kebijakan kinerja %s (masih dipakai pelanggaran)", pol.Name),
		})
		respondJSON(w, http.StatusOK, map[string]any{
			"message":     "kebijakan masih dipakai pelanggaran, dinonaktifkan (bukan dihapus)",
			"deactivated": true,
			"policy":      updated,
		})
		return
	}

	if err := h.queries.DeletePerformancePolicy(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus kebijakan kinerja")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "performance_policy",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus kebijakan kinerja %s", pol.Name),
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"message":     "kebijakan kinerja berhasil dihapus",
		"deactivated": false,
	})
}

// ── Scores ───────────────────────────────────────────────────────────────────

// ListScores — GET /api/hr/performance/scores?month=&branch_id=&q=
// Returns every active employee with their monthly score (default 100 when no
// score row exists) and violation count.
func (h *PerformanceHandler) ListScores(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	month, err := firstOfMonthDate(q.Get("month"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
		return
	}
	monthStr := month.Format("2006-01-02")

	var args []any
	args = append(args, monthStr)
	conds := []string{"e.status = 'active'"}

	if v := strings.TrimSpace(q.Get("branch_id")); v != "" {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf("e.branch_id = $%d::uuid", len(args)))
	}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		args = append(args, "%"+strings.ToLower(v)+"%")
		conds = append(conds, fmt.Sprintf("(lower(e.full_name) LIKE $%d OR lower(e.employee_code) LIKE $%d)", len(args), len(args)))
	}

	where := "WHERE " + strings.Join(conds, " AND ")

	sql := fmt.Sprintf(`
		SELECT
		    e.id, e.full_name, e.employee_code, e.branch_id, b.name AS branch_name,
		    COALESCE(s.score, 100) AS score,
		    COALESCE(vc.cnt, 0) AS violation_count
		FROM employees e
		JOIN branches b ON b.id = e.branch_id
		LEFT JOIN performance_scores s
		       ON s.employee_id = e.id AND s.period_month = $1::date
		LEFT JOIN (
		    SELECT employee_id, COUNT(*) AS cnt
		    FROM performance_violations
		    WHERE date >= $1::date AND date < ($1::date + INTERVAL '1 month')
		    GROUP BY employee_id
		) vc ON vc.employee_id = e.id
		%s
		ORDER BY score ASC, e.full_name`, where)

	rows, err := h.pool.Query(ctx, sql, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil skor kinerja")
		return
	}
	defer rows.Close()

	type scoreRow struct {
		ID             pgtype.UUID `json:"id"`
		FullName       string      `json:"full_name"`
		EmployeeCode   string      `json:"employee_code"`
		BranchID       pgtype.UUID `json:"branch_id"`
		BranchName     string      `json:"branch_name"`
		Score          int32       `json:"score"`
		ViolationCount int64       `json:"violation_count"`
	}

	items := []scoreRow{}
	for rows.Next() {
		var x scoreRow
		if err := rows.Scan(&x.ID, &x.FullName, &x.EmployeeCode, &x.BranchID, &x.BranchName, &x.Score, &x.ViolationCount); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca skor kinerja")
			return
		}
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca skor kinerja")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"month": month.Format("2006-01"), "data": items})
}

// EmployeePerformance — GET /api/hr/employees/:id/performance?month=
// Returns the monthly score (default 100) plus the violation breakdown.
func (h *PerformanceHandler) EmployeePerformance(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	empID := pgtype.UUID{Bytes: id, Valid: true}

	month, err := firstOfMonthDate(r.URL.Query().Get("month"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
		return
	}
	pgMonth := pgtype.Date{Time: month, Valid: true}

	score := int32(100)
	if s, err := h.queries.GetPerformanceScore(ctx, &db.GetPerformanceScoreParams{
		EmployeeID:  empID,
		PeriodMonth: pgMonth,
	}); err == nil {
		score = s.Score
	} else if !errors.Is(err, pgx.ErrNoRows) {
		respondError(w, http.StatusInternalServerError, "gagal mengambil skor kinerja")
		return
	}

	viols, err := h.queries.ListViolationsForEmployeeMonth(ctx, &db.ListViolationsForEmployeeMonthParams{
		EmployeeID: empID,
		Date:       pgMonth,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil rincian pelanggaran")
		return
	}
	if viols == nil {
		viols = []*db.ListViolationsForEmployeeMonthRow{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"month":      month.Format("2006-01"),
		"score":      score,
		"violations": viols,
	})
}

// ── Violations ───────────────────────────────────────────────────────────────

type manualViolationBody struct {
	EmployeeID string `json:"employee_id"`
	PolicyID   string `json:"policy_id"` // optional — links the violation to a named 'manual' policy
	Date       string `json:"date"`
	Points     int32  `json:"points"`
	Note       string `json:"note"`
}

// CreateManualViolation — POST /api/hr/performance/violations
func (h *PerformanceHandler) CreateManualViolation(w http.ResponseWriter, r *http.Request) {
	var body manualViolationBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	empUUID, err := parseUUID(strings.TrimSpace(body.EmployeeID))
	if err != nil {
		respondError(w, http.StatusBadRequest, "karyawan tidak valid")
		return
	}
	if body.Points <= 0 {
		respondError(w, http.StatusBadRequest, "poin harus lebih dari 0")
		return
	}
	dateStr := strings.TrimSpace(body.Date)
	d, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "format tanggal tidak valid (YYYY-MM-DD)")
		return
	}
	body.Note = strings.TrimSpace(body.Note)

	// Optional link to a named 'manual' policy (from the policy catalog).
	var policyID pgtype.UUID
	if s := strings.TrimSpace(body.PolicyID); s != "" {
		pu, perr := parseUUID(s)
		if perr != nil {
			respondError(w, http.StatusBadRequest, "kebijakan tidak valid")
			return
		}
		policyID = pgtype.UUID{Bytes: pu, Valid: true}
	}

	ctx := r.Context()
	empID := pgtype.UUID{Bytes: empUUID, Valid: true}
	createdBy := middleware.UserIDFromCtx(ctx)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	v, err := qtx.InsertManualViolation(ctx, &db.InsertManualViolationParams{
		EmployeeID: empID,
		PolicyID:   policyID,
		Date:       pgtype.Date{Time: d, Valid: true},
		Points:     body.Points,
		Note:       pgtype.Text{String: body.Note, Valid: body.Note != ""},
		CreatedBy:  pgtype.UUID{Bytes: createdBy, Valid: createdBy != [16]byte{}},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pelanggaran manual")
		return
	}

	if err := service.RecomputeScore(ctx, qtx, empID, d); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung ulang skor")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      createdBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "performance_violation",
		EntityID:    v.ID.Bytes,
		Description: fmt.Sprintf("Menambah pelanggaran manual (-%d poin) tanggal %s", body.Points, dateStr),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, v)
}

// DeleteViolation — DELETE /api/hr/performance/violations/:id
func (h *PerformanceHandler) DeleteViolation(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	reason := strings.TrimSpace(r.URL.Query().Get("reason"))

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	v, err := h.queries.GetViolationByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pelanggaran tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil pelanggaran")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	if err := qtx.DeleteViolation(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus pelanggaran")
		return
	}

	if err := service.RecomputeScore(ctx, qtx, v.EmployeeID, v.Date.Time); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung ulang skor")
		return
	}

	desc := "Menghapus pelanggaran kinerja"
	if reason != "" {
		desc = fmt.Sprintf("%s: %s", desc, reason)
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "performance_violation",
		EntityID:    id,
		Description: desc,
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"message": "pelanggaran berhasil dihapus"})
}

// ResetAutoViolations — DELETE /api/hr/performance/violations/auto?from=&to=
// Deletes all auto-generated violations in the date range so evaluation can be re-run cleanly.
// Manual violations are never touched.
func (h *PerformanceHandler) ResetAutoViolations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	fromStr := strings.TrimSpace(q.Get("from"))
	toStr := strings.TrimSpace(q.Get("to"))
	if fromStr == "" || toStr == "" {
		respondError(w, http.StatusBadRequest, "parameter from dan to wajib diisi (YYYY-MM-DD)")
		return
	}
	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "format from tidak valid")
		return
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "format to tidak valid")
		return
	}

	if err := h.queries.DeleteAutoViolationsForRange(ctx, &db.DeleteAutoViolationsForRangeParams{
		From: pgtype.Date{Time: from, Valid: true},
		To:   pgtype.Date{Time: to, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus pelanggaran otomatis")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "pelanggaran otomatis berhasil direset"})
}

// Evaluate — POST /api/hr/performance/evaluate?from=&to=
// Manual backfill: runs the evaluation engine over a date range.
func (h *PerformanceHandler) Evaluate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	fromStr := strings.TrimSpace(q.Get("from"))
	toStr := strings.TrimSpace(q.Get("to"))
	if fromStr == "" {
		respondError(w, http.StatusBadRequest, "parameter from wajib diisi (YYYY-MM-DD)")
		return
	}
	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "format from tidak valid (YYYY-MM-DD)")
		return
	}
	to := from
	if toStr != "" {
		to, err = time.Parse("2006-01-02", toStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format to tidak valid (YYYY-MM-DD)")
			return
		}
	}
	if to.Before(from) {
		respondError(w, http.StatusBadRequest, "tanggal to harus setelah atau sama dengan from")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	if err := qtx.DeleteAutoViolationsForRange(ctx, &db.DeleteAutoViolationsForRangeParams{
		From: pgtype.Date{Time: from, Valid: true},
		To:   pgtype.Date{Time: to, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mereset pelanggaran otomatis")
		return
	}

	// Reconcile absent records for each past day in the range before evaluating.
	// Without this, employees with no attendance record (missed nightly reconcile)
	// are invisible to the evaluation engine.
	yesterday := time.Now().AddDate(0, 0, -1)
	reconcileTo := to
	if reconcileTo.After(yesterday) {
		reconcileTo = yesterday
	}
	for d := from; !d.After(reconcileTo); d = d.AddDate(0, 0, 1) {
		if _, err := service.ReconcileAbsent(ctx, qtx, d); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menjalankan rekonsiliasi absen")
			return
		}
	}

	if err := service.EvaluateRange(ctx, qtx, from, to); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menjalankan evaluasi kinerja")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "performance_violation",
		Description: fmt.Sprintf("Evaluasi kinerja %s s/d %s", fromStr, to.Format("2006-01-02")),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan hasil evaluasi")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"message": "evaluasi kinerja selesai",
		"from":    fromStr,
		"to":      to.Format("2006-01-02"),
	})
}
