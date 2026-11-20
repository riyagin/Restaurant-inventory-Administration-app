package handler

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

type LeaveHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewLeaveHandler(pool *pgxpool.Pool, queries *db.Queries) *LeaveHandler {
	return &LeaveHandler{pool: pool, queries: queries}
}

type leaveTypeView struct {
	*db.LeaveType
	RequiresDoctorNote bool `json:"requires_doctor_note"`
}

type leaveTypeBody struct {
	Name      string `json:"name"`
	IsPaid    *bool  `json:"is_paid"`
	UsesQuota *bool  `json:"uses_quota"`
	IsActive  *bool  `json:"is_active"`
	// RequiresDoctorNote: a day without a note costs points and a day's pay.
	RequiresDoctorNote *bool `json:"requires_doctor_note"`
}

type leaveRequestView struct {
	*db.ListLeaveRequestsRow
	HasDoctorNote      bool `json:"has_doctor_note"`
	RequiresDoctorNote bool `json:"requires_doctor_note"`
}

type createLeaveRequestBody struct {
	HasDoctorNote bool   `json:"has_doctor_note"`
	EmployeeID    string `json:"employee_id"`
	LeaveTypeID   string `json:"leave_type_id"`
	StartDate     string `json:"start_date"`
	EndDate       string `json:"end_date"`
	Reason        string `json:"reason"`
	// ConfirmBackdated is required for a start date before today.
	ConfirmBackdated bool `json:"confirm_backdated"`
}

const codeBackdatedNeedsConfirm = "backdated_requires_confirmation"

type decisionBody struct {
	Note string `json:"note"`
}

type bulkDecisionBody struct {
	IDs  []string `json:"ids"`
	Note string   `json:"note"`
}

type bulkDecisionResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

var (
	errLeaveRequestNotFound      = errors.New("pengajuan libur tidak ditemukan")
	errLeaveRequestNotPending    = errors.New("pengajuan libur sudah tidak menunggu persetujuan")
	errLeaveRequestBackdatedBulk = errors.New("pengajuan mundur tanggal harus disetujui satu per satu")
)

type manpowerDay struct {
	Date      string `json:"date"`
	Status    string `json:"status"` // "hadir" | "cuti" | "pending"
	LeaveType string `json:"leave_type,omitempty"`
	// DivisionID overrides the home division for the day; empty means none.
	ShiftID    string `json:"shift_id,omitempty"`
	DivisionID string `json:"division_id,omitempty"`
	// Pending change to today's roster; empty PendingShiftID removes the shift.
	PendingRequestID  string `json:"pending_request_id,omitempty"`
	PendingShiftID    string `json:"pending_shift_id,omitempty"`
	PendingDivisionID string `json:"pending_division_id,omitempty"`
}

type manpowerEmployee struct {
	ID              string                        `json:"id"`
	Name            string                        `json:"name"`
	Position        string                        `json:"position"`
	DivisionID      string                        `json:"division_id"`
	DivisionName    string                        `json:"division_name"`
	IsDeliveryStaff bool                          `json:"is_delivery_staff"`
	Days            []manpowerDay                 `json:"days"`
	LeaveBalance    []service.MonthlyLeaveBalance `json:"leave_balance"`
}

type manpowerDivision struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type manpowerBranch struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Shifts    []*shiftDefinition `json:"shifts"`
	Divisions []manpowerDivision `json:"divisions"`
	Employees []manpowerEmployee `json:"employees"`
}

type manpowerPlanningResponse struct {
	StartDate string           `json:"start_date"`
	Today     string           `json:"today"`
	Dates     []string         `json:"dates"`
	Branches  []manpowerBranch `json:"branches"`
}

type setQuotaBody struct {
	Month     string `json:"month"` // YYYY-MM
	QuotaDays int32  `json:"quota_days"`
}
