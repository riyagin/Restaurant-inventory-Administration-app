package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// HistoryWindowDays is how far back an employee's attendance history goes, on
// the kiosk and on the HR employee page alike.
const HistoryWindowDays = 30

// HistoryDay is one calendar day of an employee's attendance as the server
// holds it. Every day of the window is present, even with no record, so the
// employee sees a gap rather than a list that silently skips it.
type HistoryDay struct {
	Date              string  `json:"date"`   // YYYY-MM-DD (WIB work date)
	Status            *string `json:"status"` // present | absent | leave | holiday; nil = no record
	CheckIn           *string `json:"check_in"`
	CheckOut          *string `json:"check_out"`
	CheckInSource     *string `json:"check_in_source"`
	CheckOutSource    *string `json:"check_out_source"`
	CheckInVerified   bool    `json:"check_in_verified"`
	CheckOutVerified  bool    `json:"check_out_verified"`
	IsLate            bool    `json:"is_late"`
	LateMinutes       int     `json:"late_minutes"`
	IsEarlyLeave      bool    `json:"is_early_leave"`
	EarlyLeaveMinutes int     `json:"early_leave_minutes"`
	IsMissingCheckout bool    `json:"is_missing_checkout"`
	IsMissingCheckin  bool    `json:"is_missing_checkin"`
	IsHalfDay         bool    `json:"is_half_day"`
	ExcusedBy         *string `json:"excused_by"` // permit that shaped the day
	Shift             *string `json:"shift"`      // "Pagi 08:00–16:00"
	LeaveType         *string `json:"leave_type"`
	// For an Alpha day: "quota" (paid from the leave allowance) or "unpaid".
	Alpha *string `json:"alpha"`
}

// HistorySummary totals the window.
type HistorySummary struct {
	Days            int `json:"days"`
	Present         int `json:"present"`
	Late            int `json:"late"`
	LateMinutes     int `json:"late_minutes"`
	EarlyLeave      int `json:"early_leave"`
	MissingCheckout int `json:"missing_checkout"`
	Alpha           int `json:"alpha"`
	AlphaUnpaid     int `json:"alpha_unpaid"`
	Leave           int `json:"leave"`
}

// AttendanceHistory is an employee's last HistoryWindowDays days, newest first.
type AttendanceHistory struct {
	From    string         `json:"from"`
	To      string         `json:"to"`
	Days    []HistoryDay   `json:"days"`
	Summary HistorySummary `json:"summary"`
}

// SlotVerified reports whether a check-in/out time is anchored to the server
// clock: a manager's manual value (no punch behind it) is the server's own
// record; a punch is verified unless its time came from an unchecked device
// clock (see device_punch.go). Fingerprint imports carry the scanner's clock.
func SlotVerified(at pgtype.Timestamptz, src, timeSource pgtype.Text) bool {
	if !at.Valid {
		return false
	}
	if src.Valid && src.String == "manual" && !timeSource.Valid {
		return true
	}
	return timeSource.Valid && timeSource.String != PunchTimeDevice
}

// GetAttendanceHistory builds the window ending today (WIB). conn runs the
// read; q is used for the Alpha allowance (same connection or pool).
func GetAttendanceHistory(ctx context.Context, conn db.DBTX, q *db.Queries, employeeID pgtype.UUID, today time.Time) (*AttendanceHistory, error) {
	to := dateOnlyUTC(today)
	from := to.AddDate(0, 0, -(HistoryWindowDays - 1))

	rows, err := conn.Query(ctx, `
		SELECT d::date,
		       ar.status, ar.check_in, ar.check_out, ar.check_in_source, ar.check_out_source,
		       COALESCE(ar.is_late, false), COALESCE(ar.late_minutes, 0),
		       COALESCE(ar.is_early_leave, false), COALESCE(ar.early_leave_minutes, 0),
		       COALESCE(ar.is_missing_checkout, false), COALESCE(ar.is_missing_checkin, false),
		       COALESCE(ar.is_half_day, false), ar.excused_by,
		       sd.name, sd.start1, sd.end1, sd.start2, sd.end2,
		       lv.name,
		       (SELECT p.time_source FROM attendance_punches p
		        WHERE p.employee_id = $1 AND p.punched_at = ar.check_in LIMIT 1),
		       (SELECT p.time_source FROM attendance_punches p
		        WHERE p.employee_id = $1 AND p.punched_at = ar.check_out LIMIT 1)
		FROM generate_series($2::date, $3::date, interval '1 day') d
		LEFT JOIN attendance_records ar ON ar.employee_id = $1 AND ar.date = d::date
		LEFT JOIN shift_assignments sa ON sa.employee_id = $1 AND sa.date = d::date
		LEFT JOIN shift_definitions sd ON sd.id = sa.shift_id
		LEFT JOIN LATERAL (
		  SELECT lt.name FROM leave_requests lr
		  JOIN leave_types lt ON lt.id = lr.leave_type_id
		  WHERE lr.employee_id = $1 AND lr.status = 'approved'
		    AND d::date BETWEEN lr.start_date AND lr.end_date
		  LIMIT 1
		) lv ON true
		ORDER BY d DESC`,
		employeeID, pgtype.Date{Time: from, Valid: true}, pgtype.Date{Time: to, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("attendance history: %w", err)
	}
	defer rows.Close()

	h := &AttendanceHistory{From: from.Format("2006-01-02"), To: to.Format("2006-01-02")}
	for rows.Next() {
		var (
			d                                 time.Time
			status, inSrc, outSrc, excused    pgtype.Text
			checkIn, checkOut                 pgtype.Timestamptz
			lateMin, earlyMin                 int32
			shiftName, leaveType, inTS, outTS pgtype.Text
			start1, end1, start2, end2        pgtype.Time
			day                               HistoryDay
		)
		if err := rows.Scan(&d, &status, &checkIn, &checkOut, &inSrc, &outSrc,
			&day.IsLate, &lateMin, &day.IsEarlyLeave, &earlyMin,
			&day.IsMissingCheckout, &day.IsMissingCheckin, &day.IsHalfDay, &excused,
			&shiftName, &start1, &end1, &start2, &end2, &leaveType, &inTS, &outTS); err != nil {
			return nil, fmt.Errorf("attendance history scan: %w", err)
		}
		day.Date = d.Format("2006-01-02")
		day.Status = textPtr(status)
		day.CheckIn, day.CheckOut = wibPtr(checkIn), wibPtr(checkOut)
		day.CheckInSource, day.CheckOutSource = textPtr(inSrc), textPtr(outSrc)
		day.CheckInVerified = SlotVerified(checkIn, inSrc, inTS)
		day.CheckOutVerified = SlotVerified(checkOut, outSrc, outTS)
		day.LateMinutes, day.EarlyLeaveMinutes = int(lateMin), int(earlyMin)
		day.ExcusedBy = textPtr(excused)
		day.LeaveType = textPtr(leaveType)
		if shiftName.Valid {
			s := fmt.Sprintf("%s %s–%s", shiftName.String, clock(start1), clock(end1))
			if start2.Valid && end2.Valid {
				s += fmt.Sprintf(" / %s–%s", clock(start2), clock(end2))
			}
			day.Shift = &s
		}
		h.Days = append(h.Days, day)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// How each Alpha day was paid for (service/alpha.go), per calendar month.
	alphaOutcome := map[string]string{}
	for m := MonthStart(from); !m.After(to); m = m.AddDate(0, 1, 0) {
		am, err := AlphaForMonth(ctx, q, employeeID, m)
		if err != nil {
			return nil, err
		}
		for _, ad := range am.Days {
			outcome := "unpaid"
			if ad.Covered {
				outcome = "quota"
			}
			alphaOutcome[ad.Date.Format("2006-01-02")] = outcome
		}
	}

	s := &h.Summary
	for i := range h.Days {
		day := &h.Days[i]
		s.Days++
		if day.Status == nil {
			continue
		}
		switch *day.Status {
		case "present":
			s.Present++
		case "leave":
			s.Leave++
		case "absent":
			s.Alpha++
			if o, ok := alphaOutcome[day.Date]; ok {
				o := o
				day.Alpha = &o
				if o == "unpaid" {
					s.AlphaUnpaid++
				}
			}
		}
		if day.IsLate {
			s.Late++
			s.LateMinutes += day.LateMinutes
		}
		if day.IsEarlyLeave {
			s.EarlyLeave++
		}
		if day.IsMissingCheckout {
			s.MissingCheckout++
		}
	}
	return h, nil
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}

func wibPtr(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	v := InWIB(t.Time).Format(time.RFC3339)
	return &v
}

// clock formats a TIME column as HH:MM.
func clock(t pgtype.Time) string {
	if !t.Valid {
		return "--:--"
	}
	min := t.Microseconds / 60_000_000
	return fmt.Sprintf("%02d:%02d", min/60, min%60)
}
