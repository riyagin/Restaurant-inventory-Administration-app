package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type HRWagesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewHRWagesHandler(pool *pgxpool.Pool, queries *db.Queries) *HRWagesHandler {
	return &HRWagesHandler{pool: pool, queries: queries}
}

func validComponentType(t string) bool {
	switch t {
	case "allowance", "bonus", "deduction":
		return true
	}
	return false
}

// ── Wage Components (master catalog) ─────────────────────────────────────────

// ListComponents — GET /api/hr/wage-components?active=1
func (h *HRWagesHandler) ListComponents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var (
		comps []*db.WageComponent
		err   error
	)
	if r.URL.Query().Get("active") == "1" {
		comps, err = h.queries.ListActiveWageComponents(ctx)
	} else {
		comps, err = h.queries.ListWageComponents(ctx)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil komponen gaji")
		return
	}
	if comps == nil {
		comps = []*db.WageComponent{}
	}
	respondJSON(w, http.StatusOK, comps)
}

type wageComponentBody struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	IsFixed  *bool  `json:"is_fixed"`
	IsActive *bool  `json:"is_active"`
}

// CreateComponent — POST /api/hr/wage-components
func (h *HRWagesHandler) CreateComponent(w http.ResponseWriter, r *http.Request) {
	var body wageComponentBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Type = strings.TrimSpace(body.Type)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama komponen wajib diisi")
		return
	}
	if !validComponentType(body.Type) {
		respondError(w, http.StatusBadRequest, "tipe komponen harus allowance, bonus, atau deduction")
		return
	}
	isFixed := true
	if body.IsFixed != nil {
		isFixed = *body.IsFixed
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	ctx := r.Context()
	comp, err := h.queries.CreateWageComponent(ctx, &db.CreateWageComponentParams{
		Name:     body.Name,
		Type:     body.Type,
		IsFixed:  isFixed,
		IsActive: isActive,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama komponen sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat komponen gaji")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "wage_component",
		EntityID:    comp.ID.Bytes,
		Description: fmt.Sprintf("Membuat komponen gaji %s", comp.Name),
	})

	respondJSON(w, http.StatusCreated, comp)
}

// UpdateComponent — PUT /api/hr/wage-components/:id
func (h *HRWagesHandler) UpdateComponent(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body wageComponentBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Type = strings.TrimSpace(body.Type)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama komponen wajib diisi")
		return
	}
	if !validComponentType(body.Type) {
		respondError(w, http.StatusBadRequest, "tipe komponen harus allowance, bonus, atau deduction")
		return
	}
	isFixed := true
	if body.IsFixed != nil {
		isFixed = *body.IsFixed
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	ctx := r.Context()
	comp, err := h.queries.UpdateWageComponent(ctx, &db.UpdateWageComponentParams{
		Name:     body.Name,
		Type:     body.Type,
		IsFixed:  isFixed,
		IsActive: isActive,
		ID:       pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "komponen gaji tidak ditemukan")
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama komponen sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui komponen gaji")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "wage_component",
		EntityID:    id,
		Description: fmt.Sprintf("Memperbarui komponen gaji %s", comp.Name),
	})

	respondJSON(w, http.StatusOK, comp)
}

// DeleteComponent — DELETE /api/hr/wage-components/:id
// Deletes only when unreferenced; otherwise deactivates (is_active = false).
func (h *HRWagesHandler) DeleteComponent(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	comp, err := h.queries.GetWageComponentByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "komponen gaji tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil komponen gaji")
		return
	}

	refs, err := h.queries.CountWageComponentReferences(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa penggunaan komponen")
		return
	}

	if refs > 0 {
		// Referenced by wage structures — deactivate instead of deleting.
		updated, err := h.queries.SetWageComponentActive(ctx, &db.SetWageComponentActiveParams{
			IsActive: false,
			ID:       pgID,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menonaktifkan komponen gaji")
			return
		}
		_ = service.LogActivity(ctx, h.queries, service.LogParams{
			UserID:      middleware.UserIDFromCtx(ctx),
			Username:    middleware.UsernameFromCtx(ctx),
			Action:      "UPDATE",
			EntityType:  "wage_component",
			EntityID:    id,
			Description: fmt.Sprintf("Menonaktifkan komponen gaji %s (masih dipakai struktur gaji)", comp.Name),
		})
		respondJSON(w, http.StatusOK, map[string]any{
			"message":     "komponen masih dipakai struktur gaji, dinonaktifkan (bukan dihapus)",
			"deactivated": true,
			"component":   updated,
		})
		return
	}

	if err := h.queries.DeleteWageComponent(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus komponen gaji")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "wage_component",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus komponen gaji %s", comp.Name),
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"message":     "komponen gaji berhasil dihapus",
		"deactivated": false,
	})
}

// ── Employee Wage Structures ─────────────────────────────────────────────────

// wageStructureView is a wage structure plus its expanded components.
type wageStructureView struct {
	*db.WageStructure
	Components []*db.ListEmployeeWageComponentsRow `json:"components"`
}

func (h *HRWagesHandler) loadComponents(ctx context.Context, structureID pgtype.UUID) ([]*db.ListEmployeeWageComponentsRow, error) {
	comps, err := h.queries.ListEmployeeWageComponents(ctx, structureID)
	if err != nil {
		return nil, err
	}
	if comps == nil {
		comps = []*db.ListEmployeeWageComponentsRow{}
	}
	return comps, nil
}

// GetCurrentWage — GET /api/hr/employees/:id/wage
func (h *HRWagesHandler) GetCurrentWage(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	empID := pgtype.UUID{Bytes: id, Valid: true}

	ws, err := h.queries.GetCurrentOpenWageStructure(ctx, empID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusOK, nil)
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil struktur gaji")
		return
	}

	comps, err := h.loadComponents(ctx, ws.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil komponen struktur gaji")
		return
	}

	respondJSON(w, http.StatusOK, wageStructureView{WageStructure: ws, Components: comps})
}

// GetWageHistory — GET /api/hr/employees/:id/wage/history
func (h *HRWagesHandler) GetWageHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	empID := pgtype.UUID{Bytes: id, Valid: true}

	versions, err := h.queries.ListWageStructuresByEmployee(ctx, empID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat struktur gaji")
		return
	}

	out := make([]wageStructureView, 0, len(versions))
	for _, ws := range versions {
		comps, err := h.loadComponents(ctx, ws.ID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil komponen struktur gaji")
			return
		}
		out = append(out, wageStructureView{WageStructure: ws, Components: comps})
	}

	respondJSON(w, http.StatusOK, out)
}

type wageVersionBody struct {
	BaseSalary          int64  `json:"base_salary"`
	WorkingDaysPerMonth int32  `json:"working_days_per_month"`
	EffectiveDate       string `json:"effective_date"`
	Components          []struct {
		ComponentID string `json:"component_id"`
		Amount      int64  `json:"amount"`
	} `json:"components"`
}

// CreateWageVersion — POST /api/hr/employees/:id/wage
func (h *HRWagesHandler) CreateWageVersion(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	empID := pgtype.UUID{Bytes: id, Valid: true}

	var body wageVersionBody
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
	effStr := strings.TrimSpace(body.EffectiveDate)
	if effStr == "" {
		respondError(w, http.StatusBadRequest, "tanggal berlaku wajib diisi")
		return
	}
	effDate, err := time.Parse("2006-01-02", effStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "format tanggal berlaku tidak valid")
		return
	}

	components := make([]service.WageComponentInput, 0, len(body.Components))
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
		components = append(components, service.WageComponentInput{
			ComponentID: pgtype.UUID{Bytes: cid, Valid: true},
			Amount:      c.Amount,
		})
	}

	ctx := r.Context()

	// Verify the employee exists for a clear 404.
	if _, err := h.queries.GetEmployeeByID(ctx, empID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	createdBy := middleware.UserIDFromCtx(ctx)
	ws, err := service.CreateWageVersion(ctx, qtx, service.CreateWageVersionParams{
		EmployeeID:    empID,
		BaseSalary:    body.BaseSalary,
		WorkingDays:   body.WorkingDaysPerMonth,
		EffectiveDate: effDate,
		CreatedBy:     pgtype.UUID{Bytes: createdBy, Valid: createdBy != [16]byte{}},
		Components:    components,
	})
	if err != nil {
		if errors.Is(err, service.ErrEffectiveDateNotAfter) {
			respondError(w, http.StatusBadRequest, "tanggal berlaku harus setelah versi struktur gaji yang aktif saat ini")
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505":
				respondError(w, http.StatusConflict, "sudah ada struktur gaji dengan tanggal berlaku yang sama")
				return
			case "23503":
				respondError(w, http.StatusBadRequest, "komponen gaji tidak ditemukan")
				return
			}
		}
		respondError(w, http.StatusInternalServerError, "gagal menyimpan struktur gaji")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      createdBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "wage_structure",
		EntityID:    ws.ID.Bytes,
		Description: fmt.Sprintf("Membuat versi struktur gaji baru (berlaku %s)", effStr),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	comps, err := h.loadComponents(ctx, ws.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil komponen struktur gaji")
		return
	}

	respondJSON(w, http.StatusCreated, wageStructureView{WageStructure: ws, Components: comps})
}
