package main

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"time"
)

// ── Simulation constants ─────────────────────────────────────────────────────

var wib = time.FixedZone("WIB", 7*3600)

const (
	simYear   = 2026
	simMonth  = time.October
	dailyRate = int64(100_000)
	// Device clock ran fast on these days (record error the system cannot see).
	driftMinutes = 12
)

var driftDays = map[int]bool{28: true, 29: true, 30: true}

// Branch schedule mirrored from the live "Warung Jambu" config.
const (
	branchStart = 8*60 + 30  // 08:30
	branchEnd   = 21*60 + 30 // 21:30
	graceMin    = 5
	earlyTolMin = 10
)

type shiftDef struct {
	Code, Name     string
	S1, E1, S2, E2 int // minutes since midnight; S2/E2 = -1 when single segment
	Break          int
}

func (s shiftDef) Start() int { return s.S1 }
func (s shiftDef) End() int {
	if s.E2 >= 0 {
		return s.E2
	}
	return s.E1
}
func (s shiftDef) Paid() int {
	p := s.E1 - s.S1
	if s.S2 >= 0 {
		p += s.E2 - s.S2
	}
	return p - s.Break
}

// Mirrored from the live Warung Jambu shift_definitions.
var shifts = map[string]shiftDef{
	"OPN":  {"OPN", "Shift Opening", 510, 1170, -1, -1, 0},
	"MID":  {"MID", "Shift Middle", 570, 1320, -1, -1, 60},
	"LONG": {"LONG", "Longshift", 510, 1320, -1, -1, 0},
	"JMP":  {"JMP", "Shift Jumping", 510, 840, 990, 1320, 0},
}

// ── Model ────────────────────────────────────────────────────────────────────

type simEmployee struct {
	DeviceID string // the user ID enrolled on the fingerprint device
	DBCode   string // employees.employee_code in the app
	Name     string
	Shift    string // usual shift
	Story    string // what this employee's month is designed to exercise
	specs    map[int]daySpec
	offDays  []int
}

type daySpec struct {
	Kind        string // "" (work) | "absent" | "leave"
	LateBy      int    // >0: arrive this many minutes after shift start
	ArriveAt    string // "HH:MM" explicit true arrival
	DepartAt    string // "HH:MM" explicit true departure
	EarlyBy     int    // >0: depart this many minutes before shift end
	DepartNext  string // "HH:MM" departure after midnight (next calendar day)
	NoOut       bool   // forgot to punch out
	NoIn        bool   // forgot to punch in
	NoPunch     bool   // worked, punched nothing
	SkipLastJMP bool   // split shift: forgot the final punch of the night
	NoRoster    bool   // worked their usual shift but no shift_assignment exists
	LeavePunch  [2]string
	Tags        []string
	Corr        *correction
	// Permit approved the day before: late_arrival | early_departure | off_site.
	Permit string
}

type correction struct {
	Kind string // no_punch | half_late | half_early | set_checkout
	At   string // "HH:MM" corrected time (WIB)
	Note string
}

type punch struct {
	DeviceID string
	T        time.Time // wall-clock time as the device recorded it (WIB)
}

type dayTruth struct {
	Emp      *simEmployee
	Day      int
	Date     time.Time
	Kind     string // work | leave | absent
	Shift    string
	Rostered bool
	Arrive   *time.Time
	Depart   *time.Time
	PunchIn  bool
	PunchOut bool
	Punches  []punch
	Tags     []string
	Corr     *correction
	Permit   string
	Exp      expectation
}

func day(d int) time.Time { return time.Date(simYear, simMonth, d, 0, 0, 0, 0, wib) }
func at(d int, hhmm string) time.Time {
	var h, m int
	fmt.Sscanf(hhmm, "%d:%d", &h, &m)
	return day(d).Add(time.Duration(h*60+m) * time.Minute)
}
func atMin(d, min int) time.Time { return day(d).Add(time.Duration(min) * time.Minute) }
func daysInMonth() int           { return time.Date(simYear, simMonth+1, 0, 0, 0, 0, 0, wib).Day() }

// ── Cast ─────────────────────────────────────────────────────────────────────

func cast() []*simEmployee {
	sp := func(kv ...any) map[int]daySpec {
		m := map[int]daySpec{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(int)] = kv[i+1].(daySpec)
		}
		return m
	}
	tag := func(t ...string) []string { return t }
	return []*simEmployee{
		{DeviceID: "101", Name: "Andi Pratama", Shift: "OPN", Story: "Control: normal attendance, some double taps"},
		{DeviceID: "102", Name: "Budi Santoso", Shift: "OPN", Story: "Late at every points-tier boundary",
			specs: sp(
				2, daySpec{LateBy: 6, Tags: tag("late_tier1")},
				5, daySpec{LateBy: 11, Tags: tag("late_tier2")},
				8, daySpec{LateBy: 21, Tags: tag("late_tier3")},
				12, daySpec{LateBy: 30, Tags: tag("late_exactly_30")},
				15, daySpec{LateBy: 31, Tags: tag("late_over_30")},
				22, daySpec{LateBy: 75, Tags: tag("late_75")},
				27, daySpec{LateBy: 4, Tags: tag("late_within_grace")},
			)},
		{DeviceID: "103", Name: "Citra Lestari", Shift: "OPN", Story: "Early leave (inside and outside tolerance)",
			specs: sp(
				6, daySpec{EarlyBy: 45, Tags: tag("early_leave_45")},
				14, daySpec{EarlyBy: 5, Tags: tag("early_within_tolerance")},
				23, daySpec{EarlyBy: 15, Tags: tag("early_leave_15")},
			)},
		{DeviceID: "104", Name: "Dewi Anggraini", Shift: "LONG", Story: "Forgot check-out; one corrected by manager",
			specs: sp(
				7, daySpec{NoOut: true, Tags: tag("forgot_checkout")},
				20, daySpec{NoOut: true, Tags: tag("forgot_checkout", "corrected_checkout"),
					Corr: &correction{Kind: "set_checkout", At: "21:40", Note: "Lupa absen pulang, dikonfirmasi supervisor"}},
			)},
		{DeviceID: "105", Name: "Eko Wahyudi", Shift: "OPN", Story: "Forgot check-in (evening punch only)",
			specs: sp(
				9, daySpec{NoIn: true, Tags: tag("forgot_checkin")},
				21, daySpec{NoIn: true, Tags: tag("forgot_checkin")},
			)},
		{DeviceID: "106", Name: "Fajar Nugroho", Shift: "LONG", Story: "Worked but never punched (corrected)",
			specs: sp(
				13, daySpec{NoPunch: true, Tags: tag("no_punch_worked"), Corr: &correction{Kind: "no_punch", Note: "Hadir, mesin tidak terbaca"}},
				16, daySpec{NoPunch: true, Tags: tag("no_punch_worked"), Corr: &correction{Kind: "no_punch", Note: "Hadir, lupa absen"}},
			)},
		{DeviceID: "107", Name: "Gilang Ramadhan", Shift: "OPN", Story: "6 scattered absences (grace = 4)",
			specs: sp(
				3, daySpec{Kind: "absent", Tags: tag("absent")},
				8, daySpec{Kind: "absent", Tags: tag("absent")},
				14, daySpec{Kind: "absent", Tags: tag("absent")},
				18, daySpec{Kind: "absent", Tags: tag("absent")},
				24, daySpec{Kind: "absent", Tags: tag("absent_beyond_grace")},
				29, daySpec{Kind: "absent", Tags: tag("absent_beyond_grace")},
			)},
		{DeviceID: "108", Name: "Hana Putri", Shift: "OPN", Story: "3-day absence streak + 1 more",
			specs: sp(
				12, daySpec{Kind: "absent", Tags: tag("absent_streak")},
				13, daySpec{Kind: "absent", Tags: tag("absent_streak")},
				14, daySpec{Kind: "absent", Tags: tag("absent_streak")},
				26, daySpec{Kind: "absent", Tags: tag("absent")},
			)},
		{DeviceID: "109", Name: "Indra Kusuma", Shift: "MID", Story: "Arrived 12:40 with Izin Masuk Siang; 12:15 without a permit",
			specs: sp(
				15, daySpec{ArriveAt: "12:40", Permit: "late_arrival", Tags: tag("arrived_after_noon", "permit_late_arrival")},
				22, daySpec{ArriveAt: "12:15", Tags: tag("arrived_after_noon", "no_permit_review")},
			)},
		{DeviceID: "110", Name: "Joko Susilo", Shift: "OPN", Story: "Went home sick 11:30 with Izin Pulang Awal; 11:50 without",
			specs: sp(
				19, daySpec{DepartAt: "11:30", Permit: "early_departure", Tags: tag("left_before_noon", "permit_early_departure")},
				25, daySpec{DepartAt: "11:50", Tags: tag("left_before_noon", "no_permit_review")},
			)},
		{DeviceID: "111", Name: "Kiki Amelia", Shift: "LONG", Story: "Checked out after midnight, late next day",
			specs: sp(
				10, daySpec{DepartNext: "00:20", Tags: tag("checkout_after_midnight")},
				11, daySpec{LateBy: 35, Tags: tag("late_after_overnight")},
				24, daySpec{DepartNext: "00:05", Tags: tag("checkout_after_midnight")},
			)},
		{DeviceID: "112", Name: "Lina Marlina", Shift: "JMP", Story: "Split shift: forgot final punch",
			specs: sp(
				4, daySpec{SkipLastJMP: true, Tags: tag("split_forgot_final_punch")},
				17, daySpec{LateBy: 8, Tags: tag("late_tier1")},
			)},
		{DeviceID: "113", Name: "Mira Handayani", Shift: "OPN", Story: "Dropped in on an approved leave day",
			specs: sp(
				18, daySpec{Kind: "leave", LeavePunch: [2]string{"10:02", "14:05"}, Tags: tag("punch_on_leave_day")},
			)},
		{DeviceID: "114", Name: "Nanda Saputra", Shift: "OPN", Story: "No shift rostered for 2 days (worked normally)",
			specs: sp(
				6, daySpec{NoRoster: true, Tags: tag("missing_roster")},
				7, daySpec{NoRoster: true, Tags: tag("missing_roster")},
			)},
		{DeviceID: "115", Name: "Oki Firmansyah", Shift: "MID", Story: "Middle shift, 1 late; Dinas Luar all day without punching",
			specs: sp(
				2, daySpec{LateBy: 15, Tags: tag("late_tier2")},
				20, daySpec{NoPunch: true, Permit: "off_site", Tags: tag("off_site_no_punch")},
			)},
		{DeviceID: "116", Name: "Putri Ayu", Shift: "OPN", Story: "Control (hit by fast device clock 28-30 Oct)"},
		{DeviceID: "117", Name: "Rudi Hartono", Shift: "JMP", Story: "Split shift; Dinas Luar in the morning, arrived 11:00",
			specs: sp(9, daySpec{ArriveAt: "11:00", Permit: "off_site", Tags: tag("off_site_morning")})},
		{DeviceID: "118", Name: "Sari Wulandari", Shift: "LONG", Story: "Mixed: late, forgot check-out, absent, early",
			specs: sp(
				3, daySpec{LateBy: 25, Tags: tag("late_tier3")},
				9, daySpec{NoOut: true, Tags: tag("forgot_checkout")},
				21, daySpec{Kind: "absent", Tags: tag("absent")},
				25, daySpec{EarlyBy: 40, Tags: tag("early_leave_40")},
			)},
		{DeviceID: "119", Name: "Tono Wijaya", Shift: "OPN", Story: "Two separate 2-day absence streaks",
			specs: sp(
				5, daySpec{Kind: "absent", Tags: tag("absent_streak")},
				6, daySpec{Kind: "absent", Tags: tag("absent_streak")},
				30, daySpec{Kind: "absent", Tags: tag("absent_streak")},
				31, daySpec{Kind: "absent", Tags: tag("absent_streak")},
			)},
		{DeviceID: "120", DBCode: "AA120", Name: "Umar Hakim", Shift: "OPN", Story: "Device ID 120 vs employee_code AA120"},
	}
}

// ── Truth + punch generation ─────────────────────────────────────────────────

func buildTruth(rng *rand.Rand) ([]*simEmployee, []*dayTruth, []punch) {
	emps := cast()
	n := daysInMonth()
	var all []*dayTruth
	var punches []punch

	for _, e := range emps {
		if e.DBCode == "" {
			e.DBCode = e.DeviceID
		}
		if e.specs == nil {
			e.specs = map[int]daySpec{}
		}
		// 4 paid days off per month, on days with nothing planted.
		var free []int
		for d := 1; d <= n; d++ {
			if _, ok := e.specs[d]; !ok && !driftDays[d] && !e.neighboursPlanted(d) {
				free = append(free, d)
			}
		}
		rng.Shuffle(len(free), func(i, j int) { free[i], free[j] = free[j], free[i] })
		e.offDays = append([]int{}, free[:4]...)
		sort.Ints(e.offDays)
		for _, d := range e.offDays {
			e.specs[d] = daySpec{Kind: "leave"}
		}

		for d := 1; d <= n; d++ {
			s := e.specs[d]
			t := &dayTruth{Emp: e, Day: d, Date: day(d), Tags: s.Tags, Corr: s.Corr, Permit: s.Permit, Rostered: true}
			switch s.Kind {
			case "leave":
				t.Kind = "leave"
				t.Rostered = false
				if s.LeavePunch[0] != "" {
					t.Punches = append(t.Punches, punch{e.DeviceID, at(d, s.LeavePunch[0]).Add(secs(rng))},
						punch{e.DeviceID, at(d, s.LeavePunch[1]).Add(secs(rng))})
				}
			case "absent":
				t.Kind = "absent"
				t.Shift = e.Shift
			default:
				t.Kind = "work"
				t.Shift = e.Shift
				t.Rostered = !s.NoRoster
				genWorkDay(rng, t, s)
			}
			// Background noise: an occasional unplanned late arrival.
			if t.Kind == "work" && len(s.Tags) == 0 && rng.Float64() < 0.05 {
				sh := shifts[t.Shift]
				late := 6 + rng.IntN(20)
				a := atMin(d, sh.Start()+late).Add(secs(rng))
				t.Arrive = &a
				t.Punches[0].T = a
				t.Tags = append(t.Tags, "random_late")
			}
			// Device clock drift: every punch recorded on these days is shifted.
			if driftDays[d] && len(t.Punches) > 0 {
				for i := range t.Punches {
					t.Punches[i].T = t.Punches[i].T.Add(driftMinutes * time.Minute)
				}
				t.Tags = append(t.Tags, "device_clock_drift")
			}
			// Double taps (harmless if dedup works).
			var withDup []punch
			for _, p := range t.Punches {
				withDup = append(withDup, p)
				if rng.Float64() < 0.08 {
					withDup = append(withDup, punch{p.DeviceID, p.T.Add(time.Duration(2+rng.IntN(40)) * time.Second)})
				}
			}
			t.Punches = withDup
			punches = append(punches, t.Punches...)
			all = append(all, t)
		}
	}

	// Stray IDs on the device that belong to no employee (admin/test enrolments).
	for _, s := range []struct {
		id string
		t  time.Time
	}{{"5555", at(1, "09:02")}, {"5555", at(15, "10:11")}, {"8888", at(20, "13:40")}, {"666", at(8, "07:55")}} {
		punches = append(punches, punch{s.id, s.t.Add(17 * time.Second)})
	}
	sort.SliceStable(punches, func(i, j int) bool { return punches[i].T.Before(punches[j].T) })
	return emps, all, punches
}

// neighboursPlanted keeps days off away from absence streaks so a leave day
// never silently breaks (or bridges) a planted streak.
func (e *simEmployee) neighboursPlanted(d int) bool {
	for _, x := range []int{d - 1, d + 1} {
		if s, ok := e.specs[x]; ok && s.Kind == "absent" {
			return true
		}
	}
	return false
}

func secs(rng *rand.Rand) time.Duration { return time.Duration(rng.IntN(60)) * time.Second }

func genWorkDay(rng *rand.Rand, t *dayTruth, s daySpec) {
	d := t.Day
	sh := shifts[t.Shift]
	id := t.Emp.DeviceID

	arrive := atMin(d, sh.Start()-(2+rng.IntN(19))).Add(secs(rng))
	switch {
	case s.ArriveAt != "":
		arrive = at(d, s.ArriveAt).Add(secs(rng))
	case s.LateBy > 0:
		arrive = atMin(d, sh.Start()+s.LateBy).Add(time.Duration(rng.IntN(50)) * time.Second)
	}
	depart := atMin(d, sh.End()+1+rng.IntN(18)).Add(secs(rng))
	switch {
	case s.DepartAt != "":
		depart = at(d, s.DepartAt).Add(secs(rng))
	case s.DepartNext != "":
		depart = at(d+1, s.DepartNext).Add(secs(rng))
	case s.EarlyBy > 0:
		depart = atMin(d, sh.End()-s.EarlyBy).Add(-time.Duration(rng.IntN(50)) * time.Second)
	}
	t.Arrive, t.Depart = &arrive, &depart
	t.PunchIn = !s.NoIn && !s.NoPunch
	t.PunchOut = !s.NoOut && !s.NoPunch && !s.SkipLastJMP

	if t.PunchIn {
		t.Punches = append(t.Punches, punch{id, arrive})
	}
	if sh.S2 >= 0 && !s.NoPunch && s.DepartAt == "" && s.ArriveAt == "" {
		// Split shift: out for the break and back in.
		out1 := atMin(d, sh.E1+rng.IntN(5)).Add(secs(rng))
		in2 := atMin(d, sh.S2-(1+rng.IntN(8))).Add(secs(rng))
		t.Punches = append(t.Punches, punch{id, out1}, punch{id, in2})
	}
	if t.PunchOut {
		t.Punches = append(t.Punches, punch{id, depart})
	}
	if !t.PunchIn && !t.PunchOut && s.NoPunch {
		t.Punches = nil
	}
}

// ── ZKTeco attlog.dat writer ─────────────────────────────────────────────────

// formatAttlog renders punches the way ZKTeco devices export attlog.dat:
// right-aligned user ID, wall-clock datetime, device no, state, verify, workcode.
func formatAttlog(ps []punch, before time.Time) []byte {
	var out []byte
	lastDay := map[string]string{}
	for _, p := range ps {
		if !p.T.Before(before) {
			continue
		}
		state := 1
		dk := p.T.Format("2006-01-02")
		if lastDay[p.DeviceID] != dk {
			state = 0
			lastDay[p.DeviceID] = dk
		}
		out = append(out, fmt.Sprintf("%9s\t%s\t5\t%d\t1\t0\r\n", p.DeviceID, p.T.Format("2006-01-02 15:04:05"), state)...)
	}
	return out
}
