package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// diffRow is one employee-day where the system disagrees with the rulebook.
type diffRow struct {
	Code, Name, Date, Tags, Fields string
	ExpStatus, SysStatus           string
	ExpPoints, SysPoints           int
	ExpCut, SysCut                 int64
	ExpRules, SysRules             string
	ExpLate, SysLate               int
	ExpEarly, SysEarly             int
	SysIn, SysOut                  string
	Cause                          string
}

func fmtT(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(wib).Format("2006-01-02 15:04:05")
}

// deviceWall is the WIB wall clock of the stored instant: what the device showed.
func deviceWall(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(wib).Format("15:04:05")
}

func flagList(late, early int, mo, mi, np bool, hd string) string {
	var f []string
	if late > 0 {
		f = append(f, fmt.Sprintf("late %dm", late))
	}
	if early > 0 {
		f = append(f, fmt.Sprintf("early %dm", early))
	}
	if mo {
		f = append(f, "missing_checkout")
	}
	if mi {
		f = append(f, "missing_checkin")
	}
	if np {
		f = append(f, "no_punch")
	}
	if hd != "" {
		f = append(f, "half_day_"+hd)
	}
	return strings.Join(f, ", ")
}

func compare(truth []*dayTruth, snap *snapshot) []diffRow {
	var out []diffRow
	for _, t := range truth {
		e := t.Exp
		r := snap.Records[key(t.Emp.DBCode, t.Day)]
		sys := &sysRecord{Status: "(no record)"}
		if r != nil {
			sys = r
		}
		sysLate, sysEarly := 0, 0
		if sys.IsLate {
			sysLate = sys.LateMin
		}
		if sys.IsEarly {
			sysEarly = sys.EarlyMin
		}
		sysHD := ""
		if sys.HalfDay {
			sysHD = sys.HalfType
		}
		var fields []string
		if e.Status != sys.Status {
			fields = append(fields, "status")
		}
		if e.Points != sys.Points {
			fields = append(fields, "points")
		}
		if e.CutAmt != sys.CutAmt {
			fields = append(fields, "pay_cut")
		}
		if e.HalfDay == "" {
			if e.LateMin != sysLate {
				fields = append(fields, "late")
			}
			if e.EarlyMin != sysEarly {
				fields = append(fields, "early_leave")
			}
			if e.MissingOut != sys.MissOut {
				fields = append(fields, "missing_checkout")
			}
			if e.MissingIn != sys.MissIn {
				fields = append(fields, "missing_checkin")
			}
		}
		if e.NoPunch != sys.NoPunch {
			fields = append(fields, "no_punch")
		}
		if e.HalfDay != sysHD || e.HalfLost != sys.HalfLost {
			fields = append(fields, "half_day")
		}
		if len(fields) == 0 {
			continue
		}
		out = append(out, diffRow{
			Code: t.Emp.DBCode, Name: t.Emp.Name, Date: t.Date.Format("2006-01-02"),
			Tags: strings.Join(t.Tags, ","), Fields: strings.Join(fields, ","),
			ExpStatus: e.Status, SysStatus: sys.Status,
			ExpPoints: e.Points, SysPoints: sys.Points, ExpCut: e.CutAmt, SysCut: sys.CutAmt,
			ExpRules: flagList(e.LateMin, e.EarlyMin, e.MissingOut, e.MissingIn, e.NoPunch, e.HalfDay),
			SysRules: flagList(sysLate, sysEarly, sys.MissOut, sys.MissIn, sys.NoPunch, sysHD),
			ExpLate:  e.LateMin, SysLate: sysLate, ExpEarly: e.EarlyMin, SysEarly: sysEarly,
			SysIn: fmtT(sys.CheckIn), SysOut: fmtT(sys.CheckOut),
			Cause: diagnose(t, sys),
		})
	}
	return out
}

// diagnose names every mechanism behind a mismatch, from the planted tags and
// the shape of the system record. Codes match the report's findings.
func diagnose(t *dayTruth, r *sysRecord) string {
	has := func(tag string) bool {
		for _, x := range t.Tags {
			if x == tag {
				return true
			}
		}
		return false
	}
	var c []string
	add := func(s string) { c = append(c, s) }
	if t.Emp.DBCode != t.Emp.DeviceID {
		add("D1 device user ID does not match employee_code: every punch dropped, day recorded absent")
	}
	for _, rule := range r.Rules {
		if strings.HasPrefix(rule, "Mangkir") && r.Status != "absent" {
			add("D7 absence penalty from the nightly run survived the later import (day is now present)")
			break
		}
	}
	if (r.IsLate && r.LateMin >= 300) || (r.IsEarly && r.EarlyMin >= 600) {
		if has("corrected_checkout") {
			add("D2 manager correction recomputed a stored check-in that is 7 h off (.dat timezone)")
		} else {
			add("D2 cumulative re-import recomputed stored times that are 7 h off: huge late/early, full-day cut")
		}
	}
	if has("checkout_after_midnight") {
		add("D3 check-out after 00:00 is filed under the next day: this day becomes missing check-out")
	}
	if has("late_after_overnight") {
		add("D3 the 00:xx check-out from last night became today's check-in: the real late arrival disappears")
	}
	if has("arrived_after_noon") {
		add("D4 fixed 12:00 split: arrival after noon read as a check-out: missing check-in, lateness never counted")
	}
	if has("left_before_noon") {
		add("D4 fixed 12:00 split: departure before noon read as a check-in: missing check-out instead of early leave")
	}
	if has("split_forgot_final_punch") {
		add("D4 split shift: the 16:xx back-from-break punch read as the check-out: 5 h early leave + pay cut")
	}
	if has("late_exactly_30") {
		add("D5 config: policy 'lebih dari 30 menit' has threshold 30, so exactly 30 min late costs 10 pts instead of 8")
	}
	if has("punch_on_leave_day") {
		add("D6 punch on an approved leave day overwrites leave with present + late/early + pay cut")
	}
	if has("missing_roster") {
		add("R1 no shift assignment: branch hours 08:30-21:30 used: false early leave + pay cut")
	}
	if has("device_clock_drift") {
		add("R2 device clock 12 min fast: false late arrivals (system cannot detect)")
	}
	if has("no_punch_worked") && r.Status == "absent" {
		add("R3 worked without punching: absent until a manager corrects it")
	}
	if len(c) == 0 && r.Status == "present" && t.Exp.Points > 0 && r.Points == 0 {
		add("D8 imported after its nightly evaluation: anomaly flagged but the violation was never created")
	}
	if len(c) == 0 && r.Status == "absent" && t.Kind == "absent" && t.Exp.Points != r.Points {
		add("D9 grace/streak counted phantom absences (days not yet imported when the nightly job ran)")
	}
	if len(c) == 0 {
		return "UNPLANNED: investigate"
	}
	return strings.Join(c, " | ")
}

func writeCSV(path string, header []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	f.Write([]byte{0xEF, 0xBB, 0xBF}) // Excel opens UTF-8 correctly with a BOM
	w := csv.NewWriter(f)
	w.Write(header)
	w.WriteAll(rows)
	return w.Error()
}

func itoa(i int) string  { return fmt.Sprint(i) }
func i64(i int64) string { return fmt.Sprint(i) }
func yn(b bool) string {
	if b {
		return "yes"
	}
	return ""
}

func writeOutputs(out string, emps []*simEmployee, truth []*dayTruth, punches []punch, snapA, snapA2, snapPre, snapB *snapshot, runLog []string, label string) error {
	// 1. Ground truth
	var gt [][]string
	for _, t := range truth {
		var ps []string
		for _, p := range t.Punches {
			ps = append(ps, p.T.Format("01-02 15:04:05"))
		}
		shift := t.Shift
		if t.Kind == "work" && !t.Rostered {
			shift += " (not rostered)"
		}
		corr := ""
		if t.Corr != nil {
			corr = t.Corr.Kind + " " + t.Corr.At
		}
		e := t.Exp
		gt = append(gt, []string{t.Emp.DeviceID, t.Emp.DBCode, t.Emp.Name, t.Date.Format("2006-01-02"), t.Kind, shift,
			fmtT(t.Arrive), fmtT(t.Depart), strings.Join(ps, " | "), strings.Join(t.Tags, ","), corr,
			e.Status, flagList(e.LateMin, e.EarlyMin, e.MissingOut, e.MissingIn, e.NoPunch, e.HalfDay), itoa(e.HalfLost),
			itoa(e.Points), e.RuleString(), itoa(e.CutMin), i64(e.CutAmt)})
	}
	if err := writeCSV(filepath.Join(out, "1_ground_truth.csv"),
		[]string{"device_id", "employee_code", "name", "date", "what_happened", "shift", "real_arrival", "real_departure",
			"punches_recorded_by_device", "planted_errors", "manager_correction", "expected_status", "expected_anomalies",
			"half_day_min", "expected_points", "rules", "expected_cut_min", "expected_cut_rp"}, gt); err != nil {
		return err
	}

	// 2. System records (pipeline B final and A)
	sysRows := func(snap *snapshot) [][]string {
		var keys []string
		for k := range snap.Records {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var rows [][]string
		for _, k := range keys {
			r := snap.Records[k]
			rows = append(rows, []string{r.Code, r.Date.Format("2006-01-02"), r.Status,
				deviceWall(r.CheckIn), fmtT(r.CheckIn), r.InSrc, deviceWall(r.CheckOut), fmtT(r.CheckOut), r.OutSrc,
				yn(r.IsLate), itoa(r.LateMin), yn(r.IsEarly), itoa(r.EarlyMin), yn(r.MissOut), yn(r.MissIn), yn(r.NoPunch),
				r.HalfType, itoa(r.HalfLost), itoa(r.Points), strings.Join(r.Rules, "; "), itoa(r.CutMin), i64(r.CutAmt)})
		}
		return rows
	}
	hdr := []string{"employee_code", "date", "status", "check_in_device_clock", "check_in_stored_as_WIB (shown in app)", "check_in_source",
		"check_out_device_clock", "check_out_stored_as_WIB (shown in app)", "check_out_source", "late", "late_min", "early_leave", "early_min",
		"missing_checkout", "missing_checkin", "no_punch", "half_day", "half_day_min", "points", "violations", "cut_min", "cut_rp"}
	if err := writeCSV(filepath.Join(out, "2_system_records_B_final.csv"), hdr, sysRows(snapB)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "2_system_records_A_weekly.csv"), hdr, sysRows(snapA)); err != nil {
		return err
	}

	// 3. Differences
	diffsB := compare(truth, snapB)
	diffsA := compare(truth, snapA)
	diffRows := func(ds []diffRow) [][]string {
		var rows [][]string
		for _, d := range ds {
			rows = append(rows, []string{d.Code, d.Name, d.Date, d.Fields, d.Tags, d.ExpStatus, d.SysStatus, d.ExpRules, d.SysRules,
				itoa(d.ExpPoints), itoa(d.SysPoints), i64(d.ExpCut), i64(d.SysCut), d.SysIn, d.SysOut, d.Cause})
		}
		return rows
	}
	dh := []string{"employee_code", "name", "date", "differs_in", "planted_errors", "expected_status", "system_status",
		"expected_anomalies", "system_anomalies", "expected_points", "system_points", "expected_cut_rp", "system_cut_rp",
		"check_in_shown_WIB", "check_out_shown_WIB", "cause"}
	if err := writeCSV(filepath.Join(out, "3_differences_B_final.csv"), dh, diffRows(diffsB)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "3_differences_A_weekly.csv"), dh, diffRows(diffsA)); err != nil {
		return err
	}
	diffsA2 := compare(truth, snapA2)
	if err := writeCSV(filepath.Join(out, "3_differences_A_after_adjustment.csv"), dh, diffRows(diffsA2)); err != nil {
		return err
	}

	// 4. Employee summary
	var sums []empSum
	var sumRows [][]string
	for _, e := range emps {
		pts, cut, half := 0, int64(0), 0
		for _, t := range truth {
			if t.Emp == e {
				pts += t.Exp.Points
				cut += t.Exp.CutAmt
				half += t.Exp.HalfLost
			}
		}
		sysHalf := 0
		for _, r := range snapB.Records {
			if r.Code == e.DBCode {
				sysHalf += r.HalfLost
			}
		}
		nd := 0
		for _, d := range diffsB {
			if d.Code == e.DBCode {
				nd++
			}
		}
		es := 100 - pts
		if es < 0 {
			es = 0
		}
		s := empSum{e.DBCode, e.DeviceID, e.Name, e.Story, es, snapA.Scores[e.DBCode], snapPre.Scores[e.DBCode], snapB.Scores[e.DBCode],
			cut, snapA.Cuts[e.DBCode], snapB.Cuts[e.DBCode], half, sysHalf, nd, snapA2.Scores[e.DBCode], snapA2.Cuts[e.DBCode]}
		sums = append(sums, s)
		sumRows = append(sumRows, []string{s.Code, s.DeviceID, s.Name, s.Story, itoa(s.ExpScore), itoa(s.ScoreB), itoa(s.ScoreA), itoa(s.ScoreA2),
			i64(s.ExpCut), i64(s.CutB), i64(s.CutA), i64(s.CutA2), itoa(s.ExpHalf), itoa(s.SysHalf), itoa(s.Diffs)})
	}
	if err := writeCSV(filepath.Join(out, "4_employee_summary.csv"),
		[]string{"employee_code", "device_id", "name", "scenario", "expected_score", "system_score_B", "system_score_A_weekly", "system_score_A_after_adjustment",
			"expected_late_early_cut_rp", "system_cut_B", "system_cut_A", "system_cut_A_after_adjustment", "expected_half_day_min",
			"system_half_day_min", "days_differing_B"}, sumRows); err != nil {
		return err
	}

	// 5. Device-side checks an HR analyst can run on the raw file.
	known := map[string]bool{}
	for _, e := range emps {
		known[e.DBCode] = true
	}
	unmatched := map[string]int{}
	for _, p := range punches {
		if !known[p.DeviceID] {
			unmatched[p.DeviceID]++
		}
	}
	var drift []driftDay
	for d := 1; d <= daysInMonth(); d++ {
		var offs []float64
		dd := driftDay{Date: day(d).Format("2006-01-02")}
		for _, t := range truth {
			if t.Day != d || t.Kind != "work" || len(t.Punches) == 0 || !t.PunchIn {
				continue
			}
			first := t.Punches[0].T
			sh := shifts[t.Shift]
			offs = append(offs, first.Sub(atMin(d, sh.Start())).Minutes())
			dd.Present++
			if r := snapB.Records[key(t.Emp.DBCode, d)]; r != nil && r.IsLate {
				dd.Late++
			}
		}
		sort.Float64s(offs)
		if len(offs) > 0 {
			dd.MedianOffsetMin = offs[len(offs)/2]
		}
		drift = append(drift, dd)
	}

	// Results bundle for the HTML report.
	res := map[string]any{
		"employees": sums, "diffsB": diffsB, "diffsA": diffsA, "unmatched": unmatched, "drift": drift,
		"punches": len(punches), "employeeDays": len(truth), "runLog": runLog,
		"diffsA2": len(diffsA2), "label": label,
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "results.json"), b, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "run_log.txt"), []byte(strings.Join(runLog, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return writeReport(out, res, sums, diffsB, diffsA, unmatched, drift, truth)
}

type empSum struct {
	Code, DeviceID, Name, Story        string
	ExpScore, ScoreA, ScorePre, ScoreB int
	ExpCut, CutA, CutB                 int64
	ExpHalf, SysHalf, Diffs            int
	ScoreA2                            int
	CutA2                              int64
}
type driftDay struct {
	Date            string
	Present, Late   int
	MedianOffsetMin float64
}
