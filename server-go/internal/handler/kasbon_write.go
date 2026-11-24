package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Create — POST /api/hr/kasbons
func (h *KasbonHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body kasbonBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	empID, err := parseUUID(strings.TrimSpace(body.EmployeeID))
	if err != nil {
		respondError(w, http.StatusBadRequest, "karyawan tidak valid")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, pgtype.UUID{Bytes: empID, Valid: true}) {
		return
	}
	fundID, err := parseUUID(strings.TrimSpace(body.FundSourceAccountID))
	if err != nil {
		respondError(w, http.StatusBadRequest, "sumber dana tidak valid")
		return
	}
	if body.Amount <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah kasbon harus lebih dari 0")
		return
	}
	body.Details = strings.TrimSpace(body.Details)
	if body.Details == "" {
		respondError(w, http.StatusBadRequest, "keterangan kasbon wajib diisi")
		return
	}
	body.SendingMethod = strings.TrimSpace(body.SendingMethod)
	if body.SendingMethod == "" {
		respondError(w, http.StatusBadRequest, "metode pengiriman wajib diisi")
		return
	}
	resolutionMonth, err := parseMonth(body.ResolutionMonth)
	if err != nil {
		respondError(w, http.StatusBadRequest, "bulan penyelesaian tidak valid")
		return
	}

	requestDateRaw := service.TodayWIB() // the request is dated on the WIB business day
	if err := service.ValidateResolutionWindow(requestDateRaw, resolutionMonth); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	installments, err := buildInstallments(body.Amount, requestDateRaw, resolutionMonth, body.Installments)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	pgEmpID := pgtype.UUID{Bytes: empID, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgEmpID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	if _, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: fundID, Valid: true}); err != nil {
		respondError(w, http.StatusBadRequest, "akun sumber dana tidak ditemukan")
		return
	}

	// Kebijakan Kasbon: window, PKWTT, no running kasbon, cap, installments and
	// the score gate. The track is frozen on the kasbon for the approver.
	track, score, err := h.checkKasbonPolicy(r, emp, pgtype.UUID{}, requestDateRaw, body.Amount, installments)
	if err != nil {
		if !policyRefusal(w, err) {
			respondError(w, http.StatusInternalServerError, "gagal memeriksa kebijakan kasbon")
		}
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	year := requestDateRaw.Year()
	maxSeq, err := qtx.GetMaxKasbonSeqForYear(ctx, fmt.Sprintf("%04d", year))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat nomor kasbon")
		return
	}
	number := service.GenerateKasbonNumber(year, maxSeq)

	createdBy := middleware.UserIDFromCtx(ctx)
	k, err := qtx.CreateKasbon(ctx, &db.CreateKasbonParams{
		KasbonNumber:        number,
		EmployeeID:          pgEmpID,
		Amount:              body.Amount,
		Details:             body.Details,
		SendingMethod:       body.SendingMethod,
		FundSourceAccountID: pgtype.UUID{Bytes: fundID, Valid: true},
		RequestDate:         pgtype.Date{Time: requestDateRaw, Valid: true},
		ResolutionMonth:     pgtype.Date{Time: resolutionMonth, Valid: true},
		CreatedBy:           pgtype.UUID{Bytes: createdBy, Valid: createdBy != [16]byte{}},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kasbon")
		return
	}

	for _, ins := range installments {
		if _, err := qtx.CreateKasbonInstallment(ctx, &db.CreateKasbonInstallmentParams{
			KasbonID: k.ID,
			DueMonth: pgtype.Date{Time: service.FirstOfMonth(ins.DueMonth), Valid: true},
			Amount:   ins.Amount,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan cicilan kasbon")
			return
		}
	}

	if err := qtx.SetKasbonPolicyCheck(ctx, &db.SetKasbonPolicyCheckParams{
		ID:             k.ID,
		ScoreAtRequest: pgtype.Int4{Int32: int32(score), Valid: true},
		ApprovalTrack:  pgtype.Text{String: track, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kasbon")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      createdBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "kasbon",
		EntityID:    k.ID.Bytes,
		Description: fmt.Sprintf("Mengajukan kasbon %s untuk %s (Rp %d)", number, emp.FullName, body.Amount),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, k)
}

// Update — PUT /api/hr/kasbons/:id (pending only)
func (h *KasbonHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body kasbonBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	existing, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !service.CanEdit(existing.Status) {
		respondError(w, http.StatusBadRequest, "hanya kasbon berstatus menunggu yang dapat diubah")
		return
	}

	fundID, err := parseUUID(strings.TrimSpace(body.FundSourceAccountID))
	if err != nil {
		respondError(w, http.StatusBadRequest, "sumber dana tidak valid")
		return
	}
	if body.Amount <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah kasbon harus lebih dari 0")
		return
	}
	body.Details = strings.TrimSpace(body.Details)
	if body.Details == "" {
		respondError(w, http.StatusBadRequest, "keterangan kasbon wajib diisi")
		return
	}
	body.SendingMethod = strings.TrimSpace(body.SendingMethod)
	if body.SendingMethod == "" {
		respondError(w, http.StatusBadRequest, "metode pengiriman wajib diisi")
		return
	}
	resolutionMonth, err := parseMonth(body.ResolutionMonth)
	if err != nil {
		respondError(w, http.StatusBadRequest, "bulan penyelesaian tidak valid")
		return
	}

	// Validate window/split against the original request date.
	requestDate := existing.RequestDate.Time
	if err := service.ValidateResolutionWindow(requestDate, resolutionMonth); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	installments, err := buildInstallments(body.Amount, requestDate, resolutionMonth, body.Installments)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if _, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: fundID, Valid: true}); err != nil {
		respondError(w, http.StatusBadRequest, "akun sumber dana tidak ditemukan")
		return
	}

	// Re-judge an edited request against Kebijakan Kasbon, as of its original
	// request date (so the window still holds).
	emp, err := h.queries.GetEmployeeByID(ctx, existing.EmployeeID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	track, score, err := h.checkKasbonPolicy(r, emp, existing.ID, requestDate, body.Amount, installments)
	if err != nil {
		if !policyRefusal(w, err) {
			respondError(w, http.StatusInternalServerError, "gagal memeriksa kebijakan kasbon")
		}
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	if err := qtx.SetKasbonPolicyCheck(ctx, &db.SetKasbonPolicyCheckParams{
		ID:             existing.ID,
		ScoreAtRequest: pgtype.Int4{Int32: int32(score), Valid: true},
		ApprovalTrack:  pgtype.Text{String: track, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui kasbon")
		return
	}

	updated, err := qtx.UpdateKasbon(ctx, &db.UpdateKasbonParams{
		Amount:              body.Amount,
		Details:             body.Details,
		SendingMethod:       body.SendingMethod,
		FundSourceAccountID: pgtype.UUID{Bytes: fundID, Valid: true},
		ResolutionMonth:     pgtype.Date{Time: resolutionMonth, Valid: true},
		ID:                  pgID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui kasbon")
		return
	}

	// Rebuild installments.
	if err := qtx.DeleteKasbonInstallments(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui cicilan kasbon")
		return
	}
	for _, ins := range installments {
		if _, err := qtx.CreateKasbonInstallment(ctx, &db.CreateKasbonInstallmentParams{
			KasbonID: pgID,
			DueMonth: pgtype.Date{Time: service.FirstOfMonth(ins.DueMonth), Valid: true},
			Amount:   ins.Amount,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan cicilan kasbon")
			return
		}
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "kasbon",
		EntityID:    id,
		Description: fmt.Sprintf("Memperbarui kasbon %s", updated.KasbonNumber),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, updated)
}
