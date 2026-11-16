package handler

import (
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Kebijakan HRD settings (migration 088): payroll cut-off day and the
// Kebijakan Kasbon conditions. One singleton row, edited from HR Settings.

// GetPolicySettings — GET /api/hr/policy-settings
func (h *HRWagesHandler) GetPolicySettings(w http.ResponseWriter, r *http.Request) {
	s, err := service.LoadPolicySettings(r.Context(), h.queries)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memuat pengaturan kebijakan")
		return
	}
	respondJSON(w, http.StatusOK, s)
}

type policySettingsBody struct {
	PayrollCutoffDay      int32 `json:"payroll_cutoff_day"`
	KasbonWindowStartDay  int32 `json:"kasbon_window_start_day"`
	KasbonWindowEndDay    int32 `json:"kasbon_window_end_day"`
	KasbonMaxAmount       int64 `json:"kasbon_max_amount"`
	KasbonMaxInstallments int32 `json:"kasbon_max_installments"`
	KasbonPermanentOnly   bool  `json:"kasbon_permanent_only"`
	KasbonMinScore        int32 `json:"kasbon_min_score"`
	KasbonPriorityScore   int32 `json:"kasbon_priority_score"`
}

func (b policySettingsBody) validate() string {
	switch {
	case b.PayrollCutoffDay < 0 || b.PayrollCutoffDay > 28:
		return "tanggal cut-off harus 0 (bulan kalender) atau 1–28"
	case b.KasbonWindowStartDay < 1 || b.KasbonWindowEndDay > 31 || b.KasbonWindowStartDay > b.KasbonWindowEndDay:
		return "periode pengajuan kasbon tidak valid"
	case b.KasbonMaxAmount < 0:
		return "nominal maksimal kasbon tidak boleh negatif"
	case b.KasbonMaxInstallments < 1 || b.KasbonMaxInstallments > service.MaxRepaymentMonths:
		return fmt.Sprintf("jumlah cicilan harus 1–%d", service.MaxRepaymentMonths)
	case b.KasbonMinScore < 0 || b.KasbonPriorityScore > 100 || b.KasbonMinScore > b.KasbonPriorityScore:
		return "batas poin kasbon tidak valid (minimum ≤ prioritas, 0–100)"
	}
	return ""
}

// SavePolicySettings — PUT /api/hr/policy-settings
func (h *HRWagesHandler) SavePolicySettings(w http.ResponseWriter, r *http.Request) {
	var b policySettingsBody
	if err := parseBody(r, &b); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if msg := b.validate(); msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	s, err := h.queries.UpdateHRPolicySettings(ctx, &db.UpdateHRPolicySettingsParams{
		PayrollCutoffDay:      b.PayrollCutoffDay,
		KasbonWindowStartDay:  b.KasbonWindowStartDay,
		KasbonWindowEndDay:    b.KasbonWindowEndDay,
		KasbonMaxAmount:       b.KasbonMaxAmount,
		KasbonMaxInstallments: b.KasbonMaxInstallments,
		KasbonPermanentOnly:   b.KasbonPermanentOnly,
		KasbonMinScore:        b.KasbonMinScore,
		KasbonPriorityScore:   b.KasbonPriorityScore,
		UpdatedBy:             pgtype.UUID{Bytes: userID, Valid: userID != [16]byte{}},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pengaturan kebijakan")
		return
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:     userID,
		Username:   middleware.UsernameFromCtx(ctx),
		Action:     "UPDATE",
		EntityType: "hr_policy_settings",
		Description: fmt.Sprintf("Mengubah kebijakan HRD: cut-off gaji tgl %d; kasbon tgl %d–%d, maks Rp%d, %d cicilan, poin min %d / prioritas %d, hanya PKWTT: %v",
			b.PayrollCutoffDay, b.KasbonWindowStartDay, b.KasbonWindowEndDay, b.KasbonMaxAmount,
			b.KasbonMaxInstallments, b.KasbonMinScore, b.KasbonPriorityScore, b.KasbonPermanentOnly),
	})
	respondJSON(w, http.StatusOK, s)
}
