package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// ListPositions — GET /api/hr/positions
func (h *HREmployeesHandler) ListPositions(w http.ResponseWriter, r *http.Request) {
	positions, err := h.queries.ListPositions(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data jabatan")
		return
	}
	if positions == nil {
		positions = []*db.Position{}
	}
	respondJSON(w, http.StatusOK, positions)
}

// CreatePosition — POST /api/hr/positions
func (h *HREmployeesHandler) CreatePosition(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama jabatan wajib diisi")
		return
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	pos, err := h.queries.CreatePosition(r.Context(), &db.CreatePositionParams{
		Name:     body.Name,
		IsActive: isActive,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama jabatan sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat jabatan")
		return
	}

	_ = service.LogActivity(r.Context(), h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(r.Context()),
		Username:    middleware.UsernameFromCtx(r.Context()),
		Action:      "CREATE",
		EntityType:  "hr_position",
		EntityID:    pos.ID.Bytes,
		Description: fmt.Sprintf("Membuat jabatan %s", pos.Name),
	})

	respondJSON(w, http.StatusCreated, pos)
}

// UpdatePosition — PUT /api/hr/positions/:id
func (h *HREmployeesHandler) UpdatePosition(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama jabatan wajib diisi")
		return
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	pos, err := h.queries.UpdatePosition(r.Context(), &db.UpdatePositionParams{
		Name:     body.Name,
		IsActive: isActive,
		ID:       pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jabatan tidak ditemukan")
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama jabatan sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui jabatan")
		return
	}

	_ = service.LogActivity(r.Context(), h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(r.Context()),
		Username:    middleware.UsernameFromCtx(r.Context()),
		Action:      "UPDATE",
		EntityType:  "hr_position",
		EntityID:    id,
		Description: fmt.Sprintf("Memperbarui jabatan %s", pos.Name),
	})

	respondJSON(w, http.StatusOK, pos)
}

// DeletePosition — DELETE /api/hr/positions/:id
func (h *HREmployeesHandler) DeletePosition(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	pos, err := h.queries.GetPositionByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jabatan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data jabatan")
		return
	}

	count, err := h.queries.CountEmployeesByPosition(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa karyawan")
		return
	}
	if count > 0 {
		respondError(w, http.StatusConflict, "jabatan masih dipakai oleh karyawan")
		return
	}

	if err := h.queries.DeletePosition(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus jabatan")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "hr_position",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus jabatan %s", pos.Name),
	})

	respondJSON(w, http.StatusOK, map[string]string{"message": "jabatan berhasil dihapus"})
}
