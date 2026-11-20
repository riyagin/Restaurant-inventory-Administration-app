package service

import (
	"fmt"
	"sort"
	"time"
)

// Per-date offense rows for the payslip's KINERJA table: every violation that
// cost points and every day that cost pay, with the date, the times that made
// it an offense (check-in for a late day, check-out for an early one) and both
// figures side by side. Pure, so the merge rule is unit-tested.

// Kinds of per-day pay cut, matching the POTONGAN lines they add up to.
const (
	PayCutAttendance = "attendance" // late / early: attendance_cut_deduction
	PayCutHalfDay    = "half_day"   // half_day_deduction
	PayCutLeave      = "leave"      // unpaid leave / sick without a note
	PayCutAlpha      = "alpha"      // Alpha the allowance could not cover
)

// OffenseViolation is one recorded violation with the attendance it was judged on.
type OffenseViolation struct {
	Date     time.Time
	Label    string // policy name
	RuleType string
	Points   int
	CheckIn  *time.Time
	CheckOut *time.Time
	LateMin  int
	EarlyMin int
	LostMin  int // half-day lost minutes
}

// DayPayCut is the pay one day cost, by kind.
type DayPayCut struct {
	Date   time.Time
	Kind   string
	Amount int64
	Note   string // e.g. "Sabtu, 2× gaji harian"
}

// PayslipOffense is one row of the table.
type PayslipOffense struct {
	Date   time.Time
	Label  string
	Points int   // points deducted (positive); 0 = pay only
	Amount int64 // pay cut that day for this offense; 0 = points only
}

// payCutKindFor is the pay cut a violation's rule is paid through.
func payCutKindFor(rule string) string {
	switch rule {
	case "late", "early_leave":
		return PayCutAttendance
	case "half_day_late", "half_day_early":
		return PayCutHalfDay
	case "unpaid_leave", "sick_no_note":
		return PayCutLeave
	case "absent_no_leave", "consecutive_absent":
		return PayCutAlpha
	}
	return ""
}

func hhmm(t *time.Time) string {
	if t == nil {
		return "--:--"
	}
	return InWIB(*t).Format("15:04")
}

// offenseDetail adds the times/minutes that made the day an offense.
func offenseDetail(v OffenseViolation) string {
	switch v.RuleType {
	case "late":
		return fmt.Sprintf("masuk %s, telat %d mnt", hhmm(v.CheckIn), v.LateMin)
	case "early_leave":
		return fmt.Sprintf("pulang %s, %d mnt lebih awal", hhmm(v.CheckOut), v.EarlyMin)
	case "half_day_late":
		return fmt.Sprintf("masuk %s, %s jam tidak bekerja", hhmm(v.CheckIn), hoursLabel(v.LostMin))
	case "half_day_early":
		return fmt.Sprintf("pulang %s, %s jam tidak bekerja", hhmm(v.CheckOut), hoursLabel(v.LostMin))
	case "missing_checkout":
		return fmt.Sprintf("masuk %s", hhmm(v.CheckIn))
	case "missing_checkin":
		return fmt.Sprintf("pulang %s", hhmm(v.CheckOut))
	}
	return ""
}

func hoursLabel(min int) string {
	if min%60 == 0 {
		return fmt.Sprintf("%d", min/60)
	}
	return fmt.Sprintf("%.1f", float64(min)/60)
}

// payCutLabel names a pay-only row (a cut with no violation on that day).
func payCutLabel(kind string) string {
	switch kind {
	case PayCutAttendance:
		return "Telat / pulang awal"
	case PayCutHalfDay:
		return "Setengah hari"
	case PayCutLeave:
		return "Libur tanpa gaji"
	case PayCutAlpha:
		return "Alpha, jatah libur habis"
	}
	return "Potongan"
}

// BuildPayslipOffenses merges violations and pay cuts into dated rows. A day's
// cut is shown on the first violation of the same kind that day (so a late day
// reads "-3 poin, Rp 3.125" on one line); a cut with no such violation gets a
// row of its own. Rows are in date order, violations before pay-only rows.
func BuildPayslipOffenses(violations []OffenseViolation, cuts []DayPayCut) []PayslipOffense {
	type key struct {
		day  string
		kind string
	}
	pending := map[key]*DayPayCut{}
	order := []key{}
	for i := range cuts {
		c := cuts[i]
		if c.Amount == 0 {
			continue
		}
		k := key{c.Date.Format("2006-01-02"), c.Kind}
		if p, ok := pending[k]; ok {
			p.Amount += c.Amount
			continue
		}
		pending[k] = &c
		order = append(order, k)
	}

	type row struct {
		PayslipOffense
		seq int
	}
	var rows []row
	for i, v := range violations {
		label := v.Label
		if d := offenseDetail(v); d != "" {
			label += " — " + d
		}
		r := row{PayslipOffense: PayslipOffense{Date: v.Date, Label: label, Points: v.Points}, seq: i}
		if kind := payCutKindFor(v.RuleType); kind != "" {
			k := key{v.Date.Format("2006-01-02"), kind}
			if c, ok := pending[k]; ok {
				r.Amount = c.Amount
				if c.Note != "" {
					r.Label += " (" + c.Note + ")"
				}
				delete(pending, k)
			}
		}
		rows = append(rows, r)
	}
	for i, k := range order {
		c, ok := pending[k]
		if !ok {
			continue
		}
		label := payCutLabel(c.Kind)
		if c.Note != "" {
			label += " (" + c.Note + ")"
		}
		rows = append(rows, row{PayslipOffense: PayslipOffense{Date: c.Date, Label: label, Amount: c.Amount}, seq: len(violations) + i})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].Date.Equal(rows[j].Date) {
			return rows[i].Date.Before(rows[j].Date)
		}
		return rows[i].seq < rows[j].seq
	})
	out := make([]PayslipOffense, len(rows))
	for i, r := range rows {
		out[i] = r.PayslipOffense
	}
	return out
}

var idWeekdayShort = [...]string{"Min", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab"}
var idMonthShort = [...]string{"", "Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

// offenseDateLabel renders "Sen 05 Okt".
func offenseDateLabel(t time.Time) string {
	return fmt.Sprintf("%s %02d %s", idWeekdayShort[t.Weekday()], t.Day(), idMonthShort[t.Month()])
}
