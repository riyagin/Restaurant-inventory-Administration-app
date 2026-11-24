package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

// policyRefusal maps a Kebijakan Kasbon refusal to 422; ok=false when err is
// some other error.
func policyRefusal(w http.ResponseWriter, err error) bool {
	if errors.Is(err, service.ErrKasbonPolicy) {
		respondError(w, http.StatusUnprocessableEntity, err.Error())
		return true
	}
	return false
}

// checkKasbonPolicy runs Kebijakan Kasbon for a request. excludeID is the
// kasbon being edited (zero UUID on create).
func (h *KasbonHandler) checkKasbonPolicy(r *http.Request, emp *db.GetEmployeeByIDRow, excludeID pgtype.UUID,
	requestDate time.Time, amount int64, installments []service.InstallmentInput) (string, int, error) {
	ctx := r.Context()
	settings, err := service.LoadPolicySettings(ctx, h.queries)
	if err != nil {
		return "", 0, err
	}
	if !excludeID.Valid {
		excludeID = pgtype.UUID{Valid: true}
	}
	open, err := h.queries.CountOpenKasbonsForEmployee(ctx, &db.CountOpenKasbonsForEmployeeParams{
		EmployeeID: emp.ID, ExcludeID: excludeID,
	})
	if err != nil {
		return "", 0, err
	}
	score, err := service.CurrentMonthScore(ctx, h.queries, emp.ID, requestDate)
	if err != nil {
		return "", 0, err
	}
	track, err := service.CheckKasbonEligibility(settings, service.KasbonEligibilityInput{
		RequestDate:    requestDate,
		Amount:         amount,
		EmploymentType: emp.EmploymentType,
		OpenKasbons:    int(open),
		Score:          score,
		Installments:   installments,
	})
	return track, score, err
}

// Eligibility — GET /api/hr/kasbons/eligibility?employee_id=
// Previews Kebijakan Kasbon for the form before the request is filed: the
// settings, the employee's score this month, and the first rule that would
// refuse a request today (amount and installments are judged on submit).
func (h *KasbonHandler) Eligibility(w http.ResponseWriter, r *http.Request) {
	empID, err := parseUUID(strings.TrimSpace(r.URL.Query().Get("employee_id")))
	if err != nil {
		respondError(w, http.StatusBadRequest, "karyawan tidak valid")
		return
	}
	pgEmpID := pgtype.UUID{Bytes: empID, Valid: true}
	if !requireEmployeeInScope(w, r, h.queries, pgEmpID) {
		return
	}
	ctx := r.Context()
	emp, err := h.queries.GetEmployeeByID(ctx, pgEmpID)
	if err != nil {
		respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
		return
	}
	settings, err := service.LoadPolicySettings(ctx, h.queries)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memuat kebijakan kasbon")
		return
	}
	today := service.TodayWIB()
	track, score, err := h.checkKasbonPolicy(r, emp, pgtype.UUID{}, today, 0, nil)
	reason := ""
	if err != nil {
		if !errors.Is(err, service.ErrKasbonPolicy) {
			respondError(w, http.StatusInternalServerError, "gagal memeriksa kebijakan kasbon")
			return
		}
		reason = err.Error()
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"eligible":        reason == "",
		"reason":          reason,
		"score":           score,
		"track":           track,
		"employment_type": emp.EmploymentType,
		"request_date":    today.Format("2006-01-02"),
		"settings":        settings,
	})
}

// buildInstallments validates the split (or builds a single default installment in
// resolution_month) against the total and the resolution window.
func buildInstallments(total int64, requestDate, resolutionMonth time.Time, raw []kasbonInstallmentInput) ([]service.InstallmentInput, error) {
	if len(raw) == 0 {
		return []service.InstallmentInput{{DueMonth: resolutionMonth, Amount: total}}, nil
	}
	out := make([]service.InstallmentInput, 0, len(raw))
	for _, r := range raw {
		dm, err := parseMonth(r.DueMonth)
		if err != nil {
			return nil, errors.New("format bulan cicilan tidak valid")
		}
		out = append(out, service.InstallmentInput{DueMonth: dm, Amount: r.Amount})
	}
	if err := service.ValidateInstallmentSplit(total, requestDate, out); err != nil {
		return nil, err
	}
	return out, nil
}
