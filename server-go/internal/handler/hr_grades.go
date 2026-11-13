package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Grade scale + position wage templates (migration 087). Both hang off
// HRWagesHandler: they are wage configuration, gated like the component catalog.

// ── Grade scale ──────────────────────────────────────────────────────────────

// ListGrades — GET /api/hr/performance-grades
func (h *HRWagesHandler) ListGrades(w http.ResponseWriter, r *http.Request) {
	s, err := service.LoadGradeScale(r.Context(), h.queries)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil skala grade")
		return
	}
	if s == nil {
		s = service.GradeScale{}
	}
	respondJSON(w, http.StatusOK, s)
}

// SaveGrades — PUT /api/hr/performance-grades  {grades: [{label, min_score, pay_percent}]}
// Replaces the whole scale. Payroll lines already generated keep the grade and
// amounts frozen on them; regenerate a line to apply the new scale to it.
func (h *HRWagesHandler) SaveGrades(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Grades []service.GradeTier `json:"grades"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	scale, err := service.ValidateGradeScale(body.Grades)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	if err := service.SaveGradeScale(ctx, qtx, scale); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan skala grade")
		return
	}
	parts := make([]string, 0, len(scale))
	for _, t := range scale {
		parts = append(parts, fmt.Sprintf("%s ≥%d → %d%%", t.Label, t.MinScore, t.PayPercent))
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "performance_grade",
		Description: "Mengubah skala grade kinerja: " + strings.Join(parts, ", "),
	})
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}
	respondJSON(w, http.StatusOK, scale)
}

// ── Position wage templates ──────────────────────────────────────────────────

// ListPositionTemplates — GET /api/hr/position-wage-templates
func (h *HRWagesHandler) ListPositionTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := h.queries.ListPositionWageTemplates(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil template gaji jabatan")
		return
	}
	if rows == nil {
		rows = []*db.ListPositionWageTemplatesRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

type positionTemplateView struct {
	*db.PositionWageTemplate
	DailyRate  int64                                       `json:"daily_rate"`
	Components []*db.ListPositionWageTemplateComponentsRow `json:"components"`
	Employees  []*db.ListActiveEmployeesByPositionRow      `json:"employees"`
}

// GetPositionTemplate — GET /api/hr/position-wage-templates/:positionId
// Returns null when the position has no template, so the wage form can ask
// without treating "none" as an error.
func (h *HRWagesHandler) GetPositionTemplate(w http.ResponseWriter, r *http.Request) {
	pid, err := parseUUID(chi.URLParam(r, "positionId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID jabatan tidak valid")
		return
	}
	view, err := h.loadPositionTemplate(r, pgtype.UUID{Bytes: pid, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil template gaji jabatan")
		return
	}
	if view == nil {
		respondJSON(w, http.StatusOK, nil)
		return
	}
	respondJSON(w, http.StatusOK, view)
}

func (h *HRWagesHandler) loadPositionTemplate(r *http.Request, pid pgtype.UUID) (*positionTemplateView, error) {
	ctx := r.Context()
	t, err := h.queries.GetPositionWageTemplate(ctx, pid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	comps, err := h.queries.ListPositionWageTemplateComponents(ctx, pid)
	if err != nil {
		return nil, err
	}
	if comps == nil {
		comps = []*db.ListPositionWageTemplateComponentsRow{}
	}
	emps, err := h.queries.ListActiveEmployeesByPosition(ctx, pid)
	if err != nil {
		return nil, err
	}
	if emps == nil {
		emps = []*db.ListActiveEmployeesByPositionRow{}
	}
	return &positionTemplateView{
		PositionWageTemplate: t,
		DailyRate:            service.ComputeDailyRate(t.BaseSalary, t.WorkingDaysPerMonth),
		Components:           comps,
		Employees:            emps,
	}, nil
}

type positionTemplateBody struct {
	BaseSalary          int64 `json:"base_salary"`
	WorkingDaysPerMonth int32 `json:"working_days_per_month"`
	Components          []struct {
		ComponentID string `json:"component_id"`
		Amount      int64  `json:"amount"`
	} `json:"components"`
}

// SavePositionTemplate — PUT /api/hr/position-wage-templates/:positionId
// Creates or replaces the template. Never touches any employee's pay: that only
// happens through ApplyPositionTemplate or an individual wage version.
func (h *HRWagesHandler) SavePositionTemplate(w http.ResponseWriter, r *http.Request) {
	pidRaw, err := parseUUID(chi.URLParam(r, "positionId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID jabatan tidak valid")
		return
	}
	pid := pgtype.UUID{Bytes: pidRaw, Valid: true}
	var body positionTemplateBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.BaseSalary < 0 {
		respondError(w, http.StatusBadRequest, "gaji pokok tidak boleh negatif")
		return
	}
	if body.WorkingDaysPerMonth < 1 || body.WorkingDaysPerMonth > 31 {
		respondError(w, http.StatusBadRequest, "hari kerja per bulan harus antara 1 dan 31")
		return
	}
	comps := make([]service.WageComponentInput, 0, len(body.Components))
	seen := map[string]bool{}
	for _, c := range body.Components {
		cid, err := parseUUID(strings.TrimSpace(c.ComponentID))
		if err != nil {
			respondError(w, http.StatusBadRequest, "komponen gaji tidak valid")
			return
		}
		if seen[cid.String()] {
			respondError(w, http.StatusBadRequest, "komponen gaji duplikat")
			return
		}
		seen[cid.String()] = true
		if c.Amount < 0 {
			respondError(w, http.StatusBadRequest, "nominal komponen tidak boleh negatif")
			return
		}
		comps = append(comps, service.WageComponentInput{ComponentID: pgtype.UUID{Bytes: cid, Valid: true}, Amount: c.Amount})
	}

	ctx := r.Context()
	pos, err := h.queries.GetPositionByID(ctx, pid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jabatan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil jabatan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	actor := middleware.UserIDFromCtx(ctx)
	if _, err := qtx.UpsertPositionWageTemplate(ctx, &db.UpsertPositionWageTemplateParams{
		PositionID:          pid,
		BaseSalary:          body.BaseSalary,
		WorkingDaysPerMonth: body.WorkingDaysPerMonth,
		UpdatedBy:           pgtype.UUID{Bytes: actor, Valid: actor != [16]byte{}},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan template gaji jabatan")
		return
	}
	if err := qtx.DeletePositionWageTemplateComponents(ctx, pid); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan template gaji jabatan")
		return
	}
	for _, c := range comps {
		if err := qtx.CreatePositionWageTemplateComponent(ctx, &db.CreatePositionWageTemplateComponentParams{
			PositionID: pid, WageComponentID: c.ComponentID, Amount: c.Amount,
		}); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				respondError(w, http.StatusBadRequest, "komponen gaji tidak ditemukan")
				return
			}
			respondError(w, http.StatusInternalServerError, "gagal menyimpan komponen template")
			return
		}
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      actor,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "position_wage_template",
		EntityID:    pidRaw,
		Description: fmt.Sprintf("Menyimpan template gaji jabatan %s (gaji pokok %d, %d komponen)", pos.Name, body.BaseSalary, len(comps)),
	})
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	view, err := h.loadPositionTemplate(r, pid)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil template gaji jabatan")
		return
	}
	respondJSON(w, http.StatusOK, view)
}

// DeletePositionTemplate — DELETE /api/hr/position-wage-templates/:positionId
func (h *HRWagesHandler) DeletePositionTemplate(w http.ResponseWriter, r *http.Request) {
	pidRaw, err := parseUUID(chi.URLParam(r, "positionId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID jabatan tidak valid")
		return
	}
	ctx := r.Context()
	pid := pgtype.UUID{Bytes: pidRaw, Valid: true}
	if err := h.queries.DeletePositionWageTemplate(ctx, pid); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus template gaji jabatan")
		return
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "position_wage_template",
		EntityID:    pidRaw,
		Description: "Menghapus template gaji jabatan",
	})
	respondJSON(w, http.StatusOK, map[string]any{"message": "template gaji jabatan dihapus"})
}

type applyTemplateResult struct {
	Applied []string            `json:"applied"`
	Skipped []applyTemplateSkip `json:"skipped"`
}

type applyTemplateSkip struct {
	EmployeeID string `json:"employee_id"`
	Name       string `json:"name"`
	Reason     string `json:"reason"`
}

// ApplyPositionTemplate — POST /api/hr/position-wage-templates/:positionId/apply
// {effective_date, employee_ids?}
//
// Creates a new wage version from the template for each selected active
// employee of the position (all of them when employee_ids is omitted), dated
// effective_date. The new version is exactly the template: per-person
// differences are replaced, which is why the UI lists who is reached and lets
// HR leave people out. Employees whose open version is already dated on/after
// effective_date are skipped, not failed, since a version cannot be inserted
// before the one in force. Inactive template components are left out.
func (h *HRWagesHandler) ApplyPositionTemplate(w http.ResponseWriter, r *http.Request) {
	pidRaw, err := parseUUID(chi.URLParam(r, "positionId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID jabatan tidak valid")
		return
	}
	pid := pgtype.UUID{Bytes: pidRaw, Valid: true}
	var body struct {
		EffectiveDate string    `json:"effective_date"`
		EmployeeIDs   *[]string `json:"employee_ids"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	effStr := strings.TrimSpace(body.EffectiveDate)
	effDate, err := time.Parse("2006-01-02", effStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal berlaku wajib diisi (YYYY-MM-DD)")
		return
	}
	var only map[[16]byte]bool
	if body.EmployeeIDs != nil {
		only = map[[16]byte]bool{}
		for _, s := range *body.EmployeeIDs {
			id, err := parseUUID(strings.TrimSpace(s))
			if err != nil {
				respondError(w, http.StatusBadRequest, "ID karyawan tidak valid")
				return
			}
			only[id] = true
		}
		if len(only) == 0 {
			respondError(w, http.StatusBadRequest, "pilih minimal satu karyawan")
			return
		}
	}

	ctx := r.Context()
	tmpl, err := h.queries.GetPositionWageTemplate(ctx, pid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jabatan ini belum punya template gaji")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil template gaji jabatan")
		return
	}
	tcomps, err := h.queries.ListPositionWageTemplateComponents(ctx, pid)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil komponen template")
		return
	}
	comps := make([]service.WageComponentInput, 0, len(tcomps))
	for _, c := range tcomps {
		if c.ComponentIsActive {
			comps = append(comps, service.WageComponentInput{ComponentID: c.WageComponentID, Amount: c.Amount})
		}
	}
	emps, err := h.queries.ListActiveEmployeesByPosition(ctx, pid)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil karyawan jabatan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	actor := middleware.UserIDFromCtx(ctx)
	res := applyTemplateResult{Applied: []string{}, Skipped: []applyTemplateSkip{}}
	for _, e := range emps {
		if only != nil && !only[e.ID.Bytes] {
			continue
		}
		if e.CurrentEffectiveDate.Valid && !effDate.After(e.CurrentEffectiveDate.Time) {
			res.Skipped = append(res.Skipped, applyTemplateSkip{
				EmployeeID: uuidToString(e.ID), Name: e.FullName,
				Reason: fmt.Sprintf("struktur gaji yang berlaku sudah tertanggal %s", e.CurrentEffectiveDate.Time.Format("2006-01-02")),
			})
			continue
		}
		ws, err := service.CreateWageVersion(ctx, qtx, service.CreateWageVersionParams{
			EmployeeID:    e.ID,
			BaseSalary:    tmpl.BaseSalary,
			WorkingDays:   tmpl.WorkingDaysPerMonth,
			EffectiveDate: effDate,
			CreatedBy:     pgtype.UUID{Bytes: actor, Valid: actor != [16]byte{}},
			Components:    comps,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("gagal membuat struktur gaji untuk %s", e.FullName))
			return
		}
		_ = service.LogActivity(ctx, qtx, service.LogParams{
			UserID:      actor,
			Username:    middleware.UsernameFromCtx(ctx),
			Action:      "CREATE",
			EntityType:  "wage_structure",
			EntityID:    ws.ID.Bytes,
			Description: fmt.Sprintf("Menerapkan template gaji jabatan ke %s (berlaku %s)", e.FullName, effStr),
		})
		res.Applied = append(res.Applied, e.FullName)
	}
	if only != nil && len(res.Applied)+len(res.Skipped) < len(only) {
		respondError(w, http.StatusBadRequest, "sebagian karyawan yang dipilih tidak aktif atau bukan dari jabatan ini")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}
	respondJSON(w, http.StatusOK, res)
}
