package handler

import (
	"context"
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

// ListLeaveTypes — GET /api/hr/leave-types?active=1
func (h *LeaveHandler) ListLeaveTypes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var (
		types []*db.LeaveType
		err   error
	)
	if r.URL.Query().Get("active") == "1" {
		types, err = h.queries.ListActiveLeaveTypes(ctx)
	} else {
		types, err = h.queries.ListLeaveTypes(ctx)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil jenis libur")
		return
	}
	flags, err := h.doctorNoteFlags(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil jenis libur")
		return
	}
	out := make([]leaveTypeView, 0, len(types))
	for _, t := range types {
		out = append(out, leaveTypeView{LeaveType: t, RequiresDoctorNote: flags[t.ID.Bytes]})
	}
	respondJSON(w, http.StatusOK, out)
}

func (h *LeaveHandler) doctorNoteFlags(ctx context.Context) (map[[16]byte]bool, error) {
	rows, err := h.queries.ListLeaveTypeDoctorNoteFlags(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[[16]byte]bool, len(rows))
	for _, r := range rows {
		m[r.ID.Bytes] = r.RequiresDoctorNote
	}
	return m, nil
}

func (h *LeaveHandler) saveDoctorNoteFlag(ctx context.Context, lt *db.LeaveType, flag *bool) (leaveTypeView, error) {
	if flag != nil {
		if err := h.queries.SetLeaveTypeRequiresDoctorNote(ctx, &db.SetLeaveTypeRequiresDoctorNoteParams{
			ID: lt.ID, RequiresDoctorNote: *flag,
		}); err != nil {
			return leaveTypeView{}, err
		}
	}
	flags, err := h.doctorNoteFlags(ctx)
	if err != nil {
		return leaveTypeView{}, err
	}
	return leaveTypeView{LeaveType: lt, RequiresDoctorNote: flags[lt.ID.Bytes]}, nil
}

// CreateLeaveType — POST /api/hr/leave-types
func (h *LeaveHandler) CreateLeaveType(w http.ResponseWriter, r *http.Request) {
	var body leaveTypeBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama jenis libur wajib diisi")
		return
	}
	isPaid := true
	if body.IsPaid != nil {
		isPaid = *body.IsPaid
	}
	usesQuota := false
	if body.UsesQuota != nil {
		usesQuota = *body.UsesQuota
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	ctx := r.Context()
	lt, err := h.queries.CreateLeaveType(ctx, &db.CreateLeaveTypeParams{
		Name:      body.Name,
		IsPaid:    isPaid,
		UsesQuota: usesQuota,
		IsActive:  isActive,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama jenis libur sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat jenis libur")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "leave_type",
		EntityID:    lt.ID.Bytes,
		Description: fmt.Sprintf("Membuat jenis libur %s", lt.Name),
	})

	view, err := h.saveDoctorNoteFlag(ctx, lt, body.RequiresDoctorNote)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan jenis libur")
		return
	}
	respondJSON(w, http.StatusCreated, view)
}

// UpdateLeaveType — PUT /api/hr/leave-types/:id
func (h *LeaveHandler) UpdateLeaveType(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body leaveTypeBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama jenis libur wajib diisi")
		return
	}
	isPaid := true
	if body.IsPaid != nil {
		isPaid = *body.IsPaid
	}
	usesQuota := false
	if body.UsesQuota != nil {
		usesQuota = *body.UsesQuota
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	ctx := r.Context()
	lt, err := h.queries.UpdateLeaveType(ctx, &db.UpdateLeaveTypeParams{
		Name:      body.Name,
		IsPaid:    isPaid,
		UsesQuota: usesQuota,
		IsActive:  isActive,
		ID:        pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jenis libur tidak ditemukan")
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama jenis libur sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui jenis libur")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_type",
		EntityID:    id,
		Description: fmt.Sprintf("Memperbarui jenis libur %s", lt.Name),
	})

	view, err := h.saveDoctorNoteFlag(ctx, lt, body.RequiresDoctorNote)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan jenis libur")
		return
	}
	respondJSON(w, http.StatusOK, view)
}

// DeleteLeaveType — DELETE /api/hr/leave-types/:id (deactivates when referenced)
func (h *LeaveHandler) DeleteLeaveType(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	lt, err := h.queries.GetLeaveTypeByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jenis libur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil jenis libur")
		return
	}

	refs, err := h.queries.CountLeaveTypeReferences(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa penggunaan jenis libur")
		return
	}

	if refs > 0 {
		updated, err := h.queries.SetLeaveTypeActive(ctx, &db.SetLeaveTypeActiveParams{
			IsActive: false,
			ID:       pgID,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menonaktifkan jenis libur")
			return
		}
		_ = service.LogActivity(ctx, h.queries, service.LogParams{
			UserID:      middleware.UserIDFromCtx(ctx),
			Username:    middleware.UsernameFromCtx(ctx),
			Action:      "UPDATE",
			EntityType:  "leave_type",
			EntityID:    id,
			Description: fmt.Sprintf("Menonaktifkan jenis libur %s (masih dipakai pengajuan)", lt.Name),
		})
		respondJSON(w, http.StatusOK, map[string]any{
			"message":     "jenis libur masih dipakai pengajuan, dinonaktifkan (bukan dihapus)",
			"deactivated": true,
			"leave_type":  updated,
		})
		return
	}

	if err := h.queries.DeleteLeaveType(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus jenis libur")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "leave_type",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus jenis libur %s", lt.Name),
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"message":     "jenis libur berhasil dihapus",
		"deactivated": false,
	})
}
