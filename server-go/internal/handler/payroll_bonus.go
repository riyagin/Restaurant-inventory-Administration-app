package handler

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// BonusEligible — GET /api/hr/payroll/periods/:id/bonus-eligible?wage_component_id=
func (h *PayrollHandler) BonusEligible(w http.ResponseWriter, r *http.Request) {
	periodID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID periode tidak valid")
		return
	}
	wcID, err := parseUUID(r.URL.Query().Get("wage_component_id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "wage_component_id tidak valid")
		return
	}
	ctx := r.Context()

	rows, err := h.queries.GetBonusEligibleLines(ctx, &db.GetBonusEligibleLinesParams{
		PayrollPeriodID: pgtype.UUID{Bytes: periodID, Valid: true},
		WageComponentID: pgtype.UUID{Bytes: wcID, Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar karyawan eligible")
		return
	}
	if rows == nil {
		rows = []*db.GetBonusEligibleLinesRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// ApplyBonus — POST /api/hr/payroll/periods/:id/apply-bonus
func (h *PayrollHandler) ApplyBonus(w http.ResponseWriter, r *http.Request) {
	periodID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID periode tidak valid")
		return
	}
	var body applyBonusBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.AmountPerEmployee <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah per karyawan harus lebih dari 0")
		return
	}
	if len(body.LineComponentIDs) == 0 {
		respondError(w, http.StatusBadRequest, "pilih minimal satu karyawan")
		return
	}

	ctx := r.Context()
	pgPeriodID := pgtype.UUID{Bytes: periodID, Valid: true}

	period, err := h.queries.GetPayrollPeriodByID(ctx, pgPeriodID)
	if err != nil {
		respondError(w, http.StatusNotFound, "periode penggajian tidak ditemukan")
		return
	}
	if period.Status != "open" {
		respondError(w, http.StatusConflict, service.ErrPeriodLocked.Error())
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	wcID, err := parseUUID(body.WageComponentID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "wage_component_id tidak valid")
		return
	}
	eligible, err := qtx.GetBonusEligibleLines(ctx, &db.GetBonusEligibleLinesParams{
		PayrollPeriodID: pgPeriodID,
		WageComponentID: pgtype.UUID{Bytes: wcID, Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar eligible")
		return
	}

	type eligibleEntry struct {
		lineID        pgtype.UUID
		currentAmount int64
	}
	lcToLine := map[string]eligibleEntry{}
	for _, row := range eligible {
		lcToLine[row.LineComponentID.String()] = eligibleEntry{
			lineID:        row.LineID,
			currentAmount: row.CurrentAmount,
		}
	}

	selectedLineIDs := map[string]pgtype.UUID{}

	for _, lcIDStr := range body.LineComponentIDs {
		entry, ok := lcToLine[lcIDStr]
		if !ok {
			respondError(w, http.StatusBadRequest, "komponen tidak ditemukan dalam periode ini: "+lcIDStr)
			return
		}
		lcID, _ := parseUUID(lcIDStr)
		pgLCID := pgtype.UUID{Bytes: lcID, Valid: true}

		newAmount := entry.currentAmount + body.AmountPerEmployee
		if err := qtx.UpdatePayrollLineComponentAmount(ctx, &db.UpdatePayrollLineComponentAmountParams{
			Amount: newAmount,
			ID:     pgLCID,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui komponen bonus")
			return
		}
		selectedLineIDs[entry.lineID.String()] = entry.lineID
	}

	for _, lineID := range selectedLineIDs {
		line, err := qtx.GetPayrollLineByID(ctx, lineID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil baris penggajian")
			return
		}
		comps, err := qtx.ListPayrollLineComponents(ctx, lineID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil komponen baris")
			return
		}
		var bonusTotal int64
		for _, c := range comps {
			if c.Type == service.ComponentTypeBonus {
				bonusTotal += c.Amount
			}
		}
		grossPay := line.BaseSalary + line.AllowanceTotal + bonusTotal +
			line.OvertimeAmount + line.OvertimeHourlyAmount + line.PublicHolidayAmount
		netPay := grossPay - line.ComponentDeductionTotal - line.KasbonDeduction - line.UnpaidLeaveDeduction - line.HalfDayDeduction - line.AttendanceCutDeduction
		if err := qtx.UpdatePayrollLineTotals(ctx, &db.UpdatePayrollLineTotalsParams{
			BonusTotal: bonusTotal,
			GrossPay:   grossPay,
			NetPay:     netPay,
			ID:         lineID,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui total baris")
			return
		}
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "payroll_period",
		EntityID:    periodID,
		Description: fmt.Sprintf("Distribusi bonus ke %d karyawan pada periode %s", len(body.LineComponentIDs), period.PeriodMonth.Time.Format("2006-01")),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"updated": len(body.LineComponentIDs),
	})
}
