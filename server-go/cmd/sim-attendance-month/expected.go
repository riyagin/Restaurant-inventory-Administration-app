package main

import (
	"math"
	"strings"
)

// Independent reference implementation of the rulebook
// (Aturan-Absen-Potongan-Poin.pdf) with the live policy numbers. It works from
// what REALLY happened (truth), not from what the device recorded, so record
// errors show up as differences against the system.

type expectation struct {
	Status     string
	LateMin    int
	EarlyMin   int
	MissingOut bool
	MissingIn  bool
	NoPunch    bool
	HalfDay    string // "" | late | early
	HalfLost   int
	Points     int
	Rules      []string
	CutMin     int
	CutAmt     int64
}

// Live late tiers, interpreted by their NAMES: "1–10", "11–20", "21–30",
// "lebih dari 30" (strictly more than 30).
func expectedLatePoints(min int) (int, string) {
	switch {
	case min >= 31:
		return 10, "late >30"
	case min >= 21:
		return 8, "late 21-30"
	case min >= 11:
		return 4, "late 11-20"
	case min >= 1:
		return 2, "late 1-10"
	}
	return 0, ""
}

const (
	ptsMissingCheckout = 10
	ptsAbsent          = 30
	ptsConsecutive     = 40
	consecutiveDays    = 2
	ptsHalfDay         = 4
	absenceGraceDays   = 4
)

func roundUp15(m int) int {
	if m <= 0 {
		return 0
	}
	return (m + 14) / 15 * 15
}

func wallMin(t interface {
	Hour() int
	Minute() int
}) int {
	return t.Hour()*60 + t.Minute()
}

func computeExpected(days []*dayTruth) {
	byEmp := map[*simEmployee][]*dayTruth{}
	for _, t := range days {
		byEmp[t.Emp] = append(byEmp[t.Emp], t)
	}
	for _, list := range byEmp {
		absentSoFar, streak := 0, 0
		for _, t := range list {
			e := expectation{}
			switch t.Kind {
			case "leave":
				e.Status = "leave"
				streak = 0
			case "absent":
				e.Status = "absent"
				if absentSoFar >= absenceGraceDays {
					e.Points += ptsAbsent
					e.Rules = append(e.Rules, "absent_no_leave")
				}
				absentSoFar++
				streak++
				if streak == consecutiveDays {
					e.Points += ptsConsecutive
					e.Rules = append(e.Rules, "consecutive_absent")
				}
			case "work":
				streak = 0
				e.Status = "present"
				workDay(t, &e)
			}
			t.Exp = e
		}
	}
}

func workDay(t *dayTruth, e *expectation) {
	sh := shifts[t.Shift]
	// Dinas Luar: not judged on late / early / missing punches; present even
	// without any punch.
	if t.Permit == "off_site" {
		e.Rules = append(e.Rules, "off_site (izin)")
		return
	}
	if t.Corr != nil {
		switch t.Corr.Kind {
		case "no_punch":
			e.NoPunch = true
			e.Rules = append(e.Rules, "no_punch (tidak ada policy otomatis)")
			return
		case "half_late":
			e.HalfDay = "late"
			e.HalfLost = wallMin(at(t.Day, t.Corr.At)) - sh.Start()
			e.Points += ptsHalfDay
			e.Rules = append(e.Rules, "half_day_late")
			return
		case "half_early":
			e.HalfDay = "early"
			e.HalfLost = sh.End() - wallMin(at(t.Day, t.Corr.At))
			e.Points += ptsHalfDay
			e.Rules = append(e.Rules, "half_day_early")
			return
		}
	}

	hasIn := t.PunchIn
	hasOut := t.PunchOut || (t.Corr != nil && t.Corr.Kind == "set_checkout")
	cutLate, cutEarly := 0, 0
	if hasIn {
		in := wallMin(*t.Arrive)
		if in > sh.Start()+graceMin {
			e.LateMin = in - sh.Start()
			if t.Permit == "late_arrival" {
				// Izin Masuk Siang: a half day instead of the late tiers.
				e.HalfDay, e.HalfLost = "late", e.LateMin
				e.Points += ptsHalfDay
				e.Rules = append(e.Rules, "half_day_late (izin)")
			} else {
				p, r := expectedLatePoints(e.LateMin)
				e.Points += p
				e.Rules = append(e.Rules, r)
				cutLate = e.LateMin
			}
		}
	}
	if hasOut {
		dep := *t.Depart
		if t.Corr != nil && t.Corr.Kind == "set_checkout" {
			dep = at(t.Day, t.Corr.At)
		}
		out := int(dep.Sub(day(t.Day)).Minutes())
		if out < sh.End()-earlyTolMin {
			e.EarlyMin = sh.End() - out
			if t.Permit == "early_departure" {
				if e.HalfDay == "" {
					e.HalfDay = "early"
					e.Points += ptsHalfDay
				}
				e.HalfLost += e.EarlyMin
				e.Rules = append(e.Rules, "half_day_early (izin)")
			} else {
				e.Rules = append(e.Rules, "early_leave (tidak ada policy)")
				cutEarly = e.EarlyMin
			}
		}
	}
	if hasIn && !hasOut {
		e.MissingOut = true
		e.Points += ptsMissingCheckout
		e.Rules = append(e.Rules, "missing_checkout")
	}
	if !hasIn && hasOut {
		e.MissingIn = true
		e.Rules = append(e.Rules, "missing_checkin (tidak ada policy)")
	}
	paid := sh.Paid()
	if e.HalfDay != "" {
		cutLate, cutEarly = 0, 0 // a half day is deducted through its lost minutes
	}
	e.CutMin = roundUp15(cutLate) + roundUp15(cutEarly)
	if e.CutMin > paid {
		e.CutMin = paid
	}
	e.CutAmt = int64(math.Floor(float64(dailyRate)*float64(e.CutMin)/float64(paid) + 0.5))
}

func (e expectation) RuleString() string { return strings.Join(e.Rules, "; ") }
