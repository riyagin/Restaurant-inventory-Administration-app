package handler

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Penyesuaian Evaluasi — review, then apply, a re-evaluation of one month.
//
// Violations written by the nightly job go stale when attendance for a day
// arrives later (a fingerprint file imported afterwards, a correction). The
// preview shows exactly what re-evaluating the month would remove, add and
// re-score, plus the days with a punch on approved leave; apply runs the same
// re-evaluation for real. See service/evaluation_adjust.go.

// AdjustmentPreview — GET /api/hr/performance/adjustments?month=YYYY-MM
func (h *PerformanceHandler) AdjustmentPreview(w http.ResponseWriter, r *http.Request) {
	month, err := firstOfMonthDate(r.URL.Query().Get("month"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
		return
	}
	from, to, ok := service.EvaluationRange(month)
	if !ok {
		respondError(w, http.StatusBadRequest, "bulan ini belum memiliki hari yang selesai untuk dievaluasi")
		return
	}
	p, err := service.PreviewReevaluation(r.Context(), h.pool, from, to)
	if err != nil {
		log.Printf("adjustment preview %s: %v", month.Format("2006-01"), err)
		respondError(w, http.StatusInternalServerError, "gagal menghitung pratinjau penyesuaian")
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// AdjustmentApply — POST /api/hr/performance/adjustments/apply?month=YYYY-MM
func (h *PerformanceHandler) AdjustmentApply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	month, err := firstOfMonthDate(strings.TrimSpace(r.URL.Query().Get("month")))
	if err != nil {
		respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
		return
	}
	from, to, ok := service.EvaluationRange(month)
	if !ok {
		respondError(w, http.StatusBadRequest, "bulan ini belum memiliki hari yang selesai untuk dievaluasi")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	if err := service.ReevaluateRange(ctx, qtx, from, to); err != nil {
		log.Printf("adjustment apply %s: %v", month.Format("2006-01"), err)
		respondError(w, http.StatusInternalServerError, "gagal menerapkan penyesuaian evaluasi")
		return
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "performance_violation",
		Description: fmt.Sprintf("Penyesuaian evaluasi %s s/d %s", from.Format("2006-01-02"), to.Format("2006-01-02")),
	})
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan penyesuaian evaluasi")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{
		"message": "penyesuaian evaluasi diterapkan",
		"from":    from.Format("2006-01-02"),
		"to":      to.Format("2006-01-02"),
	})
}
