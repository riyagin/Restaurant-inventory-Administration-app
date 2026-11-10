package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/service"
)

// Late-arrival / early-departure pay cut: the per-day breakdown behind a payroll
// line's attendance_cut_deduction, and a calculator that runs the same pricing
// on hand-entered times. Both delegate to service.ComputeAttendanceCut, so the
// simulator can never disagree with what payroll deducts.

// GetLineAttendanceCuts — GET /api/hr/payroll/lines/{id}/attendance-cuts.
//
// The line's amount is frozen at generation; the breakdown is recomputed from
// attendance now. When a correction landed after generation the two differ, and
// `stale` says so — regenerating the line brings the frozen figure up to date.
func (h *PayrollHandler) GetLineAttendanceCuts(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()

	line, err := h.queries.GetPayrollLineByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris penggajian tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris penggajian")
		return
	}
	period, err := h.queries.GetPayrollPeriodByID(ctx, line.PayrollPeriodID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil periode penggajian")
		return
	}
	emp, err := h.queries.GetEmployeeByID(ctx, line.EmployeeID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	sched := service.DefaultSchedule()
	if ws, err := h.queries.GetWorkScheduleByBranch(ctx, emp.BranchID); err == nil && ws != nil {
		sched = service.ScheduleFromRow(ws)
	}

	live, err := service.AttendanceCutsForPeriod(ctx, h.queries, line.EmployeeID,
		period.StartDate.Time, period.EndDate.Time, line.DailyRate, sched)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung potongan absensi")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"employee_name":       emp.FullName,
		"employee_code":       emp.EmployeeCode,
		"daily_rate":          line.DailyRate,
		"block_minutes":       service.AttendanceCutBlockMinutes,
		"branch_paid_minutes": sched.WorkEndMinutes - sched.WorkStartMinutes,
		"line_minutes":        line.AttendanceCutMinutes,
		"line_amount":         line.AttendanceCutDeduction,
		"live":                live,
		"stale":               live.Amount != line.AttendanceCutDeduction || live.Minutes != int(line.AttendanceCutMinutes),
	})
}

type simulateAttendanceCutBody struct {
	DailyRate         int64  `json:"daily_rate"`
	Start1            string `json:"start1"` // "HH:MM"
	End1              string `json:"end1"`
	Start2            string `json:"start2"` // optional second segment (jumping shift)
	End2              string `json:"end2"`
	BreakMinutes      int    `json:"break_minutes"`
	GraceMinutes      int    `json:"grace_minutes"`
	EarlyLeaveMinutes int    `json:"early_leave_minutes"`
	CheckIn           string `json:"check_in"`  // "HH:MM", blank = no punch
	CheckOut          string `json:"check_out"` // "HH:MM", blank = no punch
}

// parseClock reads "HH:MM" as minutes since midnight. ok=false for blank input.
func parseClock(s string) (int, bool, error) {
	if s == "" {
		return 0, false, nil
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, false, err
	}
	return t.Hour()*60 + t.Minute(), true, nil
}

// SimulateAttendanceCut — POST /api/hr/payroll/attendance-cut/simulate.
//
// Runs one hypothetical day through the real anomaly detection (grace and
// early-leave tolerance included) and the real pricing.
func (h *PayrollHandler) SimulateAttendanceCut(w http.ResponseWriter, r *http.Request) {
	var body simulateAttendanceCutBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if body.DailyRate < 0 || body.BreakMinutes < 0 || body.GraceMinutes < 0 || body.EarlyLeaveMinutes < 0 {
		respondError(w, http.StatusBadRequest, "nilai tidak boleh negatif")
		return
	}

	clocks := map[string]*int{}
	parsed := map[string]bool{}
	for name, raw := range map[string]string{
		"start1": body.Start1, "end1": body.End1, "start2": body.Start2, "end2": body.End2,
		"check_in": body.CheckIn, "check_out": body.CheckOut,
	} {
		m, ok, err := parseClock(raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format jam tidak valid (HH:MM): "+name)
			return
		}
		v := m
		clocks[name] = &v
		parsed[name] = ok
	}
	if !parsed["start1"] || !parsed["end1"] || *clocks["end1"] <= *clocks["start1"] {
		respondError(w, http.StatusBadRequest, "jam masuk/pulang shift wajib diisi dan jam pulang harus setelah jam masuk")
		return
	}
	if parsed["start2"] != parsed["end2"] {
		respondError(w, http.StatusBadRequest, "segmen kedua harus diisi lengkap (mulai dan selesai) atau dikosongkan")
		return
	}

	workStart, workEnd := *clocks["start1"], *clocks["end1"]
	paid := workEnd - workStart
	if parsed["start2"] {
		if *clocks["start2"] < workEnd || *clocks["end2"] <= *clocks["start2"] {
			respondError(w, http.StatusBadRequest, "segmen kedua harus dimulai setelah segmen pertama selesai")
			return
		}
		paid += *clocks["end2"] - *clocks["start2"]
		workEnd = *clocks["end2"]
	}
	paid -= body.BreakMinutes
	if paid <= 0 {
		respondError(w, http.StatusBadRequest, "istirahat melebihi panjang shift")
		return
	}

	sched := service.Schedule{
		WorkStartMinutes:  workStart,
		WorkEndMinutes:    workEnd,
		GraceMinutes:      body.GraceMinutes,
		EarlyLeaveMinutes: body.EarlyLeaveMinutes,
	}
	day := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	st := &service.AttendanceState{Status: "present"}
	if parsed["check_in"] {
		t := day.Add(time.Duration(*clocks["check_in"]) * time.Minute)
		st.CheckIn = &t
	}
	if parsed["check_out"] {
		t := day.Add(time.Duration(*clocks["check_out"]) * time.Minute)
		st.CheckOut = &t
	}
	service.ComputeAnomalies(st, sched, true)

	late, early := 0, 0
	if st.IsLate {
		late = st.LateMinutes
	}
	if st.IsEarlyLeave {
		early = st.EarlyLeaveMinutes
	}
	res := service.ComputeAttendanceCut(body.DailyRate, paid, late, early)

	respondJSON(w, http.StatusOK, map[string]any{
		"paid_minutes":        paid,
		"block_minutes":       service.AttendanceCutBlockMinutes,
		"is_late":             st.IsLate,
		"late_minutes":        st.LateMinutes,
		"is_early_leave":      st.IsEarlyLeave,
		"early_leave_minutes": st.EarlyLeaveMinutes,
		"is_missing_checkout": st.IsMissingCheckout,
		"is_missing_checkin":  st.IsMissingCheckin,
		"is_no_punch":         st.IsNoPunch,
		"result":              res,
		"net_day_pay":         body.DailyRate - res.Amount,
	})
}
