package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Approve — POST /api/hr/kasbons/:id/approve (manager only)
// The approve body MAY adjust the installment split; it is re-validated.
func (h *KasbonHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body struct {
		Note         string                   `json:"note"`
		Installments []kasbonInstallmentInput `json:"installments"`
	}
	_ = parseBody(r, &body)

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	k, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !service.CanApprove(k.Status) {
		respondError(w, http.StatusBadRequest, "hanya kasbon berstatus menunggu yang dapat disetujui")
		return
	}

	// Optionally re-validate an adjusted split.
	var newInstallments []service.InstallmentInput
	if len(body.Installments) > 0 {
		newInstallments, err = buildInstallments(k.Amount, k.RequestDate.Time, k.ResolutionMonth.Time, body.Installments)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		settings, err := service.LoadPolicySettings(ctx, h.queries)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memuat kebijakan kasbon")
			return
		}
		if err := service.CheckKasbonInstallmentPolicy(settings, k.RequestDate.Time, newInstallments); err != nil {
			respondError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	deciderID := middleware.UserIDFromCtx(ctx)
	note := pgtype.Text{}
	if s := strings.TrimSpace(body.Note); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	if newInstallments != nil {
		if err := qtx.DeleteKasbonInstallments(ctx, pgID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui cicilan kasbon")
			return
		}
		for _, ins := range newInstallments {
			if _, err := qtx.CreateKasbonInstallment(ctx, &db.CreateKasbonInstallmentParams{
				KasbonID: pgID,
				DueMonth: pgtype.Date{Time: service.FirstOfMonth(ins.DueMonth), Valid: true},
				Amount:   ins.Amount,
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan cicilan kasbon")
				return
			}
		}
	}

	updated, err := qtx.ApproveKasbon(ctx, &db.ApproveKasbonParams{
		ApprovedBy:   pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}},
		ApprovalNote: note,
		ID:           pgID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyetujui kasbon")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "kasbon",
		EntityID:    id,
		Description: fmt.Sprintf("Menyetujui kasbon %s", updated.KasbonNumber),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, updated)
}

// Reject — POST /api/hr/kasbons/:id/reject (manager only)
func (h *KasbonHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	_ = parseBody(r, &body)

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	k, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !service.CanApprove(k.Status) {
		respondError(w, http.StatusBadRequest, "hanya kasbon berstatus menunggu yang dapat ditolak")
		return
	}

	deciderID := middleware.UserIDFromCtx(ctx)
	note := pgtype.Text{}
	if s := strings.TrimSpace(body.Note); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	updated, err := h.queries.RejectKasbon(ctx, &db.RejectKasbonParams{
		ApprovedBy:   pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}},
		ApprovalNote: note,
		ID:           pgID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menolak kasbon")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "kasbon",
		EntityID:    id,
		Description: fmt.Sprintf("Menolak kasbon %s", updated.KasbonNumber),
	})

	respondJSON(w, http.StatusOK, updated)
}

// Cancel — POST /api/hr/kasbons/:id/cancel (pending/approved only)
func (h *KasbonHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	k, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, k.EmployeeID) {
		return
	}
	if !service.CanCancel(k.Status) {
		respondError(w, http.StatusBadRequest, "kasbon hanya dapat dibatalkan saat berstatus menunggu atau disetujui")
		return
	}

	updated, err := h.queries.SetKasbonCancelled(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan kasbon")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "kasbon",
		EntityID:    id,
		Description: fmt.Sprintf("Membatalkan kasbon %s", updated.KasbonNumber),
	})

	respondJSON(w, http.StatusOK, updated)
}

// Process — POST /api/hr/kasbons/:id/process (multipart, optional photo)
// In one transaction: status → processed, debit fund source, credit Piutang Karyawan.
func (h *KasbonHandler) Process(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	k, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !service.CanProcess(k.Status) {
		respondError(w, http.StatusBadRequest, "hanya kasbon yang sudah disetujui yang dapat diproses")
		return
	}

	// Optional multipart photo evidence.
	evidence := pgtype.Text{}
	if err := r.ParseMultipartForm(20 << 20); err == nil {
		if file, header, ferr := r.FormFile("photo"); ferr == nil {
			defer file.Close()
			ext := strings.ToLower(filepath.Ext(header.Filename))
			switch ext {
			case ".jpg", ".jpeg", ".png":
			default:
				respondError(w, http.StatusBadRequest, "format file tidak didukung (jpg, jpeg, png)")
				return
			}
			filename := fmt.Sprintf("kasbon-%s-%d%s", id.String(), time.Now().Unix(), ext)
			uploadsDir := h.resolveUploadsDir()
			if err := os.MkdirAll(uploadsDir, 0755); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membuat direktori upload")
				return
			}
			dst, cerr := os.Create(filepath.Join(uploadsDir, filename))
			if cerr != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan file")
				return
			}
			if _, werr := io.Copy(dst, file); werr != nil {
				dst.Close()
				respondError(w, http.StatusInternalServerError, "gagal menulis file")
				return
			}
			dst.Close()
			evidence = pgtype.Text{String: filename, Valid: true}
		}
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	processedBy := middleware.UserIDFromCtx(ctx)
	updated, err := service.ProcessKasbon(ctx, qtx, k,
		pgtype.UUID{Bytes: processedBy, Valid: processedBy != [16]byte{}}, evidence)
	if err != nil {
		if errors.Is(err, service.ErrPiutangAccountMissing) {
			respondError(w, http.StatusInternalServerError, "akun sistem 'Piutang Karyawan' tidak ditemukan; jalankan migrasi 011")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memproses kasbon")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      processedBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "kasbon",
		EntityID:    id,
		Description: fmt.Sprintf("Memproses kasbon %s (mendebit sumber dana Rp %d)", updated.KasbonNumber, updated.Amount),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, updated)
}
