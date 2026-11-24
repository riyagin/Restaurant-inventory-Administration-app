package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

func yearParam(r *http.Request) int32 {
	if v := strings.TrimSpace(r.URL.Query().Get("year")); v != "" {
		if yr, err := strconv.Atoi(v); err == nil {
			return int32(yr)
		}
	}
	return int32(service.TodayWIB().Year())
}

// GetLeaveBalance — GET /api/hr/employees/:id/leave-balance?year=
func (h *LeaveHandler) GetLeaveBalance(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgEmpID := pgtype.UUID{Bytes: id, Valid: true}
	year := int(yearParam(r))
	if !requireEmployeeInScope(w, r, h.queries, pgEmpID) {
		return
	}

	if _, err := h.queries.GetEmployeeByID(ctx, pgEmpID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	months, err := service.MonthlyLeaveBalances(ctx, h.queries, pgEmpID, year, today())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil saldo libur")
		return
	}
	unusedClosed := 0
	for _, m := range months {
		if m.Closed {
			unusedClosed += m.UnusedDays
		}
	}
	cutoff, err := service.LeaveCutoffDay(ctx, h.queries)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengaturan periode")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"year":                year,
		"cutoff_day":          cutoff,
		"default_quota":       service.DefaultMonthlyLeaveQuota,
		"weekend_cost":        service.WeekendLeaveCost,
		"months":              months,
		"unused_closed_total": unusedClosed,
	})
}

// ListMonthlyLeaveBalances — GET /api/hr/leave-balances?month=YYYY-MM
func (h *LeaveHandler) ListMonthlyLeaveBalances(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cutoff, err := service.LeaveCutoffDay(ctx, h.queries)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengaturan periode")
		return
	}
	current := service.LeavePeriodOf(today(), cutoff)
	month := current
	if v := strings.TrimSpace(r.URL.Query().Get("month")); v != "" {
		m, err := time.Parse("2006-01", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
			return
		}
		month = m
	}

	from, to := service.LeavePeriodRange(month, cutoff)
	rows, err := h.queries.ListMonthlyLeaveBalances(ctx, &db.ListMonthlyLeaveBalancesParams{
		Month:        pgtype.Date{Time: month, Valid: true},
		FromDate:     pgtype.Date{Time: from, Valid: true},
		ToDate:       pgtype.Date{Time: to, Valid: true},
		DefaultQuota: service.DefaultMonthlyLeaveQuota,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil saldo libur")
		return
	}

	alpha, err := service.AbsentDatesByEmployeeMonth(ctx, h.queries, from, to, cutoff)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data alpha")
		return
	}

	type row struct {
		EmployeeID   pgtype.UUID `json:"employee_id"`
		EmployeeName string      `json:"employee_name"`
		EmployeeCode string      `json:"employee_code"`
		BranchName   string      `json:"branch_name"`
		service.MonthlyLeaveBalance
	}
	out := make([]row, 0, len(rows))
	for _, rr := range rows {
		out = append(out, row{
			EmployeeID:   rr.EmployeeID,
			EmployeeName: rr.EmployeeName,
			EmployeeCode: rr.EmployeeCode,
			BranchName:   rr.BranchName,
			MonthlyLeaveBalance: service.NewMonthlyLeaveBalance(month, int(rr.QuotaDays), int(rr.UsedDays),
				rr.Overridden, today(), cutoff).WithAlpha(alpha[rr.EmployeeID.Bytes][month]),
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"month":         month.Format("2006-01"),
		"current_month": current.Format("2006-01"),
		"cutoff_day":    cutoff,
		"period_start":  from.Format("2006-01-02"),
		"period_end":    to.AddDate(0, 0, -1).Format("2006-01-02"),
		"closed":        month.Before(current),
		"default_quota": service.DefaultMonthlyLeaveQuota,
		"weekend_cost":  service.WeekendLeaveCost,
		"employees":     out,
	})
}

// SetLeaveBalanceQuota — PUT /api/hr/employees/:id/leave-balance
func (h *LeaveHandler) SetLeaveBalanceQuota(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body setQuotaBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.QuotaDays < 0 {
		respondError(w, http.StatusBadRequest, "kuota libur tidak boleh negatif")
		return
	}
	month, err := time.Parse("2006-01", strings.TrimSpace(body.Month))
	if err != nil {
		respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
		return
	}

	ctx := r.Context()
	pgEmpID := pgtype.UUID{Bytes: id, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgEmpID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	if err := h.queries.UpsertLeaveMonthlyQuota(ctx, &db.UpsertLeaveMonthlyQuotaParams{
		EmployeeID: pgEmpID,
		Month:      pgtype.Date{Time: month, Valid: true},
		QuotaDays:  body.QuotaDays,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui kuota libur")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_balance",
		EntityID:    id,
		Description: fmt.Sprintf("Mengatur jatah libur %s bulan %s menjadi %d hari", emp.FullName, month.Format("01/2006"), body.QuotaDays),
	})

	months, err := service.MonthlyLeaveBalances(ctx, h.queries, pgEmpID, month.Year(), today())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil saldo libur")
		return
	}
	respondJSON(w, http.StatusOK, months[month.Month()-1])
}
