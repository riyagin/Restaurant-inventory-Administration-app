package handler

import (
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

// PayrollHandler: closed/paid periods are immutable; every mutation returns 409 when locked.
type PayrollHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	poster  *service.PayrollPoster
}

func NewPayrollHandler(pool *pgxpool.Pool, queries *db.Queries) *PayrollHandler {
	return &PayrollHandler{
		pool:    pool,
		queries: queries,
		poster:  service.NewPayrollPoster(pool, queries),
	}
}

func (h *PayrollHandler) Poster() *service.PayrollPoster { return h.poster }

type createPeriodBody struct {
	PeriodMonth string `json:"period_month"` // "YYYY-MM" or "YYYY-MM-DD"
}

type payrollLineWithGrade struct {
	*db.ListPayrollLinesForPeriodRow
	PerformanceGrade pgtype.Text `json:"performance_grade"`
	GradePayPercent  pgtype.Int4 `json:"grade_pay_percent"`
	// Alpha share of unpaid_leave_days / unpaid_leave_deduction.
	AlphaUnpaidDays int32 `json:"alpha_unpaid_days"`
	AlphaDeduction  int64 `json:"alpha_deduction"`
}

type reviewComponentInput struct {
	ID     string `json:"id"`     // payroll_line_components.id
	Amount int64  `json:"amount"` // adjusted amount
}

type reviewLineBody struct {
	OvertimeDays      float64                `json:"overtime_days"`
	OvertimeHours     float64                `json:"overtime_hours"`
	PublicHolidayDays float64                `json:"public_holiday_days"`
	Components        []reviewComponentInput `json:"components"` // adjusted bonus/allowance variable amounts
	ReviewNote        string                 `json:"review_note"`
}

type applyBonusBody struct {
	WageComponentID   string   `json:"wage_component_id"`
	AmountPerEmployee int64    `json:"amount_per_employee"` // cents
	LineComponentIDs  []string `json:"line_component_ids"`  // payroll_line_components.id
}

var errNoWageStructure = errors.New("no wage structure")
