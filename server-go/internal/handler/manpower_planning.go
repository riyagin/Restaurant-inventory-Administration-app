package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

// GetManpowerPlanning — GET /api/hr/manpower-planning?date=&days=7..31&branch_id=
func (h *LeaveHandler) GetManpowerPlanning(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var startDate time.Time
	if v := strings.TrimSpace(r.URL.Query().Get("date")); v != "" {
		var err error
		startDate, err = time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal tidak valid (YYYY-MM-DD)")
			return
		}
	} else {
		startDate = service.TodayWIB()
	}

	numDays := 7
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			numDays = n
		}
	}
	if numDays < 7 {
		numDays = 7
	}
	if numDays > 31 {
		numDays = 31
	}

	endDate := startDate.AddDate(0, 0, numDays-1)

	dates := make([]string, numDays)
	for i := range dates {
		dates[i] = startDate.AddDate(0, 0, i).Format("2006-01-02")
	}

	branchFilter := strings.TrimSpace(r.URL.Query().Get("branch_id"))

	empRows, err := h.pool.Query(ctx, `
		SELECT e.id::text, e.full_name,
		       COALESCE(e.branch_id::text, ''), COALESCE(b.name, 'Tanpa Cabang'),
		       COALESCE(p.name, ''),
		       COALESCE(e.division_id::text, ''), COALESCE(d.name, ''), e.is_delivery_staff
		FROM employees e
		LEFT JOIN branches b ON b.id = e.branch_id
		LEFT JOIN positions p ON p.id = e.position_id
		LEFT JOIN divisions d ON d.id = e.division_id
		WHERE e.status = 'active'
		  AND ($1 = '' OR e.branch_id::text = $1)
		ORDER BY COALESCE(b.name, 'Tanpa Cabang'), e.full_name
	`, branchFilter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	defer empRows.Close()

	type empInfo struct {
		id, name, branchID, branchName, position, divisionID, divisionName string
		deliveryStaff                                                      bool
	}
	var employees []empInfo
	for empRows.Next() {
		var e empInfo
		if err := empRows.Scan(&e.id, &e.name, &e.branchID, &e.branchName, &e.position,
			&e.divisionID, &e.divisionName, &e.deliveryStaff); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data karyawan")
			return
		}
		employees = append(employees, e)
	}
	if err := empRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data karyawan")
		return
	}

	startPg := pgtype.Date{Time: startDate, Valid: true}
	endPg := pgtype.Date{Time: endDate, Valid: true}
	leaveRows, err := h.pool.Query(ctx, `
		SELECT lr.employee_id::text, lr.start_date, lr.end_date, lt.name, lr.status
		FROM leave_requests lr
		JOIN leave_types lt ON lt.id = lr.leave_type_id
		WHERE lr.status IN ('approved', 'pending')
		  AND lr.start_date <= $2
		  AND lr.end_date   >= $1
	`, startPg, endPg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data libur")
		return
	}
	defer leaveRows.Close()

	// Approved requests win over pending ones on the same date.
	type leaveEntry struct {
		leaveType string
		status    string // "cuti" | "pending"
	}
	leaveLookup := map[string]map[string]leaveEntry{}
	for leaveRows.Next() {
		var empID, leaveTypeName, status string
		var sd, ed pgtype.Date
		if err := leaveRows.Scan(&empID, &sd, &ed, &leaveTypeName, &status); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data libur")
			return
		}
		entryStatus := "pending"
		if status == "approved" {
			entryStatus = "cuti"
		}
		if leaveLookup[empID] == nil {
			leaveLookup[empID] = map[string]leaveEntry{}
		}
		for cur := sd.Time; !cur.After(ed.Time); cur = cur.AddDate(0, 0, 1) {
			d := cur.Format("2006-01-02")
			if existing, ok := leaveLookup[empID][d]; ok && existing.status == "cuti" {
				continue // approved already recorded for this date — don't downgrade to pending
			}
			leaveLookup[empID][d] = leaveEntry{leaveType: leaveTypeName, status: entryStatus}
		}
	}
	if err := leaveRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data libur")
		return
	}

	var windowMonths []time.Time
	for m := service.MonthStart(startDate); !m.After(endDate); m = m.AddDate(0, 1, 0) {
		windowMonths = append(windowMonths, m)
	}
	now := today()
	balanceBy := map[string][]service.MonthlyLeaveBalance{} // employee id → one per window month
	alpha, err := service.AbsentDatesByEmployeeMonth(ctx, h.queries,
		windowMonths[0], windowMonths[len(windowMonths)-1].AddDate(0, 1, 0))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data alpha")
		return
	}
	for _, m := range windowMonths {
		rows, err := h.queries.ListMonthlyLeaveBalances(ctx, &db.ListMonthlyLeaveBalancesParams{
			Month:        pgtype.Date{Time: m, Valid: true},
			DefaultQuota: service.DefaultMonthlyLeaveQuota,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil saldo libur")
			return
		}
		for _, rr := range rows {
			empID := uuid.UUID(rr.EmployeeID.Bytes).String()
			balanceBy[empID] = append(balanceBy[empID], service.NewMonthlyLeaveBalance(
				m, int(rr.QuotaDays), int(rr.UsedDays), rr.Overridden, now).WithAlpha(alpha[rr.EmployeeID.Bytes][m]))
		}
	}

	// Requests for past days are left out; they can only be rejected, on the approvals page.
	type rosterCell struct{ shiftID, divisionID, reqID, reqShiftID, reqDivisionID string }
	roster := map[string]map[string]*rosterCell{} // employee id → date → cell
	cellFor := func(empID, d string) *rosterCell {
		if roster[empID] == nil {
			roster[empID] = map[string]*rosterCell{}
		}
		if roster[empID][d] == nil {
			roster[empID][d] = &rosterCell{}
		}
		return roster[empID][d]
	}
	rosterRows, err := h.pool.Query(ctx, `
		SELECT employee_id::text, to_char(date, 'YYYY-MM-DD'), shift_id::text, COALESCE(division_id::text, '')
		FROM shift_assignments WHERE date BETWEEN $1 AND $2`, startPg, endPg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil jadwal shift")
		return
	}
	for rosterRows.Next() {
		var empID, d, shiftID, divID string
		if err := rosterRows.Scan(&empID, &d, &shiftID, &divID); err != nil {
			rosterRows.Close()
			respondError(w, http.StatusInternalServerError, "gagal membaca jadwal shift")
			return
		}
		c := cellFor(empID, d)
		c.shiftID, c.divisionID = shiftID, divID
	}
	rosterRows.Close()
	reqRows, err := h.pool.Query(ctx, `
		SELECT id::text, employee_id::text, to_char(date, 'YYYY-MM-DD'),
		       COALESCE(shift_id::text, ''), COALESCE(division_id::text, '')
		FROM shift_change_requests
		WHERE status = 'pending' AND date BETWEEN GREATEST($1::date, $3::date) AND $2`,
		startPg, endPg, pgtype.Date{Time: now, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan shift")
		return
	}
	for reqRows.Next() {
		var id, empID, d, shiftID, divID string
		if err := reqRows.Scan(&id, &empID, &d, &shiftID, &divID); err != nil {
			reqRows.Close()
			respondError(w, http.StatusInternalServerError, "gagal membaca pengajuan shift")
			return
		}
		c := cellFor(empID, d)
		c.reqID, c.reqShiftID, c.reqDivisionID = id, shiftID, divID
	}
	reqRows.Close()

	branchMap := map[string]*manpowerBranch{}
	var branchOrder []string
	for _, e := range employees {
		if _, ok := branchMap[e.branchID]; !ok {
			branchMap[e.branchID] = &manpowerBranch{ID: e.branchID, Name: e.branchName,
				Shifts: []*shiftDefinition{}, Divisions: []manpowerDivision{}}
			branchOrder = append(branchOrder, e.branchID)
		}
		days := make([]manpowerDay, numDays)
		for i, d := range dates {
			day := manpowerDay{Date: d, Status: "hadir"}
			if entry, onLeave := leaveLookup[e.id][d]; onLeave {
				day.Status = entry.status
				day.LeaveType = entry.leaveType
			}
			if c := roster[e.id][d]; c != nil {
				day.ShiftID, day.DivisionID = c.shiftID, c.divisionID
				day.PendingRequestID, day.PendingShiftID, day.PendingDivisionID = c.reqID, c.reqShiftID, c.reqDivisionID
			}
			days[i] = day
		}
		branchMap[e.branchID].Employees = append(branchMap[e.branchID].Employees, manpowerEmployee{
			ID:              e.id,
			Name:            e.name,
			Position:        e.position,
			DivisionID:      e.divisionID,
			DivisionName:    e.divisionName,
			IsDeliveryStaff: e.deliveryStaff,
			Days:            days,
			LeaveBalance:    balanceBy[e.id],
		})
	}

	shifts, err := loadShiftDefinitions(ctx, h.pool, branchFilter, true)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar shift")
		return
	}
	for _, s := range shifts {
		if b, ok := branchMap[s.BranchID]; ok {
			b.Shifts = append(b.Shifts, s)
		}
	}
	divRows, err := h.pool.Query(ctx, `
		SELECT id::text, branch_id::text, name FROM divisions
		WHERE ($1 = '' OR branch_id::text = $1) ORDER BY name`, branchFilter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil divisi")
		return
	}
	for divRows.Next() {
		var d manpowerDivision
		var branchID string
		if err := divRows.Scan(&d.ID, &branchID, &d.Name); err != nil {
			divRows.Close()
			respondError(w, http.StatusInternalServerError, "gagal membaca divisi")
			return
		}
		if b, ok := branchMap[branchID]; ok {
			b.Divisions = append(b.Divisions, d)
		}
	}
	divRows.Close()

	branches := make([]manpowerBranch, len(branchOrder))
	for i, id := range branchOrder {
		branches[i] = *branchMap[id]
	}

	respondJSON(w, http.StatusOK, manpowerPlanningResponse{
		StartDate: startDate.Format("2006-01-02"),
		Today:     now.Format("2006-01-02"),
		Dates:     dates,
		Branches:  branches,
	})
}
