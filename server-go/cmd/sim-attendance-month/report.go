package main

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ── Finding catalogue (text) — numbers are filled in from the run ───────────

type finding struct {
	Code, Severity, Title, What, Where, Fix string
	Simulated                               bool
	RowsA, RowsB                            int
	PtsA, PtsB                              int
	RpA, RpB                                int64
}

func catalogue() []*finding {
	return []*finding{
		{Code: "D1", Severity: "Critical", Simulated: true,
			Title: "Device user IDs never match employee codes",
			What:  "The import matches the device user ID (e.g. 120) against employee_code (e.g. AA120) exactly. Every punch of a mismatched employee is dropped, the nightly job marks them absent every day, and their score collapses to 0. Your real sample file docs/attendance sample/5_attlog.dat holds 39 user IDs, all numeric; none of the 115 active employee codes is numeric, so a real import today would match nobody.",
			Where: "handler/attendance_fingerprint.go: FingerprintConfirm → GetEmployeeByCode; employees has no device-ID column",
			Fix:   "Add a device user ID per employee (per device or branch) and match on it. Block the confirm step when matched = 0 and show unmatched IDs prominently."},
		{Code: "D2", Severity: "Critical", Simulated: true,
			Title: ".dat times are stored as UTC, which is 7 hours off",
			What:  "The DAT parser uses time.Parse with no location, so a 08:11 punch is stored as 08:11 UTC (15:11 WIB). The app then shows every fingerprint time 7 hours late. Whenever the stored value is re-read (a manager correction, or re-importing the cumulative attlog.dat), anomalies are recomputed on the shifted time: about 400 min late, about 1000 min early, and a full day's pay cut. On a server whose clock is UTC the recompute happens to agree, but the display offset remains.",
			Where: "service/fingerprint_parser.go: DATFingerprintParser.Parse",
			Fix:   "Use time.ParseInLocation(layout, raw, Asia/Jakarta). Migrate existing fingerprint rows by −7 h. Add a test that round-trips a punch through the DB."},
		{Code: "D7", Severity: "Critical", Simulated: true,
			Title: "Importing does not re-evaluate: nightly absence penalties stay",
			What:  "The nightly job marks everyone without a record absent and penalises them. When the device file is imported later, those days flip to present, but their absent_no_leave / consecutive_absent violations are never removed. With a weekly import, every employee ends the month at score 0.",
			Where: "handler/attendance_fingerprint.go: FingerprintConfirm (no DeleteAutoViolationsForRecord); cmd/api/main.go nightly goroutine",
			Fix:   "After merging, call DeleteAutoViolationsForRecord for every touched record (as the correction handler already does), then re-run the month's grace/streak evaluation."},
		{Code: "D8", Severity: "High", Simulated: true,
			Title: "Days imported after their nightly run are never scored",
			What:  "Late and missing-check-out flags are written correctly by the import, but the evaluation already ran for that date, so no violation is created. Same root cause as D7. (Hidden behind D2 when the server is on WIB; visible in the UTC-server run.)",
			Where: "handler/attendance_fingerprint.go: FingerprintConfirm",
			Fix:   "Same fix as D7: evaluate touched records at the end of the import."},
		{Code: "D9", Severity: "High", Simulated: true,
			Title: "Absence grace and streak count phantom absences",
			What:  "When the nightly job runs before the import, not-yet-imported days are 'absent'. The 4-day grace and the 2-day streak rule count them, so scattered real absences get streak penalties (Gilang, 3 Oct: 40 pts) and the grace is used up early.",
			Where: "service/performance.go: evaluateRecord (CountAbsentDaysBeforeInMonth, CountConsecutiveAbsentDays)",
			Fix:   "Re-evaluate the whole month after each import (the backfill endpoint already does this correctly), or delay absence evaluation until that day's device file is in."},
		{Code: "D3", Severity: "High", Simulated: true,
			Title: "Check-outs after midnight land on the next day",
			What:  "Punches are grouped by calendar date. A 00:20 check-out after a 22:00 shift makes that day 'missing check-out' (10 pts) and becomes the next day's check-in at 00:20, which hides a real late arrival the next morning.",
			Where: "handler/attendance_fingerprint.go: grouping by p.Timestamp date",
			Fix:   "Assign punches to the rostered shift window (e.g. shift start −3 h to shift end +4 h) instead of the calendar day."},
		{Code: "D4", Severity: "High", Simulated: true,
			Title: "Fixed 12:00 check-in/check-out split",
			What:  "Punches before 12:00 are check-ins and punches after are check-outs, whatever the shift. Arriving at 12:15 becomes 'missing check-in' with no late penalty (should be 10 pts). Going home at 11:50 becomes 'missing check-out' instead of early leave. On the split shift, forgetting the last punch turns the 16:2x back-from-break punch into a 5-hour early leave with a pay cut.",
			Where: "handler/attendance_fingerprint.go: ts.Hour() < 12",
			Fix:   "Use the first and last punch inside the rostered shift window. The device state column (0 = in, 1 = out) can help."},
		{Code: "D6", Severity: "Medium", Simulated: true,
			Title: "A punch on an approved leave day erases the leave",
			What:  "Dropping in on a day off flips 'leave' to 'present'. Branch hours are then applied (no shift is rostered), so the visit is scored as late and early, with a pay cut, on a paid day off.",
			Where: "service/attendance.go: MergeAttendanceEvent (status → present)",
			Fix:   "Keep the leave status, store the punches, and flag the day for review instead of scoring it."},
		{Code: "D5", Severity: "Medium", Simulated: true,
			Title: "Policy 'Terlambat lebih dari 30 menit' starts at 30, not 31",
			What:  "Tiers pick the highest threshold ≤ minutes late, so exactly 30 minutes late scores 10 pts instead of 8 ('21–30 menit'). This is a configuration error, not code.",
			Where: "performance_policies row (dev DB): threshold_minutes = 30",
			Fix:   "Set threshold_minutes = 31 for 'Terlambat lebih dari 30 menit'."},
		{Code: "R1", Severity: "Record error", Simulated: true,
			Title: "Present days with no rostered shift",
			What:  "When a shift assignment is missing, branch hours 08:30–21:30 apply. An Opening-shift worker who leaves at 19:3x gets ~115 min early leave and a pay cut. The system gives no warning that the roster is empty.",
			Where: "service/shifts.go: ScheduleWithShift fallback",
			Fix:   "Warn on present days without a shift (import preview and payroll check)."},
		{Code: "R2", Severity: "Record error", Simulated: true,
			Title: "Device clock running fast",
			What:  "28–30 Oct the device clock ran 12 min fast. The system cannot tell, so on-time staff were marked late. It shows up in this evaluation as a jump in the daily median arrival offset (see the table below).",
			Where: "device configuration",
			Fix:   "Sync the device clock (NTP or a weekly check) and add a daily 'median arrival offset' check to the import preview."},
		{Code: "R3", Severity: "Record error", Simulated: true,
			Title: "Worked without punching",
			What:  "Correctly recorded absent until a manager uses 'present without punch'. After correction the day is present with no_punch. Works as designed.",
			Where: "—", Fix: "None needed. See C3 for the missing points rule."},
	}
}

type gap struct{ Code, Title, Detail string }

func configGaps() []gap {
	return []gap{
		{"C1", "No points rule for early leave", "The rulebook (section 1.2) says early leave costs points. There is no active early_leave policy, so it only costs pay."},
		{"C2", "No points rule for missing check-in", "Rulebook 1.4 says points. There is no missing_checkin policy (missing_checkout has 10)."},
		{"C3", "No automatic points for no-punch days", "Rulebook 1.5 says points. Only a manual policy exists (10 pts), so it depends on the manager remembering to add it."},
		{"C4", "Late tier boundary", "See D5."},
	}
}

func codeNotes() []gap {
	return []gap{
		{"N1", "Nightly job is not a schedule", "cmd/api/main.go starts a 24 h ticker at process start. The first run is 24 h after boot, and every restart (deploy, PM2) resets it, so days can be skipped silently with no catch-up."},
		{"N2", "Fresh database cannot migrate", "Migration 042 inserts a journal entry with zero lines on an empty DB and fails the journal constraint. The simulator skips 042."},
		{"N3", "Seconds are ignored", "08:35:59 counts as 08:35, which is not late with a 5-min grace. That is lenient by up to 59 s; decide whether that's intended."},
		{"N4", "Face records on a UTC server (inferred, not simulated)", "Face events arrive as correct RFC3339 +07:00 instants. On a server running in UTC, re-reading them for a correction gives 01:xx and recomputes wrongly: the mirror image of D2."},
	}
}

// ── Template data ────────────────────────────────────────────────────────────

type tagStat struct {
	Tag     string
	Days    int
	Correct int
}

type reportData struct {
	Employees, EmployeeDays, Punches, Planted int
	DiffsB, DiffsA, DiffsA2                   int
	Label                                     string
	WrongCutB, MissedCutB                     int64
	WrongCutA                                 int64
	ZeroScoresA                               int
	Findings                                  []*finding
	Gaps, Notes                               []gap
	Tags                                      []tagStat
	Emps                                      []empSum
	Diffs                                     []diffRow
	Drift                                     []driftDay
	DriftMedian                               float64
	Unmatched                                 []kv
	RunLog                                    []string
}

type kv struct {
	K string
	V int
}

func rp(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprint(v)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "−Rp " + b.String()
	}
	return "Rp " + b.String()
}

func writeReport(out string, res map[string]any, sums []empSum, diffsB, diffsA []diffRow, unmatched map[string]int, drift []driftDay, truth []*dayTruth) error {
	d := reportData{Employees: len(sums), EmployeeDays: len(truth), Punches: res["punches"].(int),
		DiffsB: len(diffsB), DiffsA: len(diffsA), Gaps: configGaps(), Notes: codeNotes(), Emps: sums, Diffs: diffsB, Drift: drift}
	d.RunLog, _ = res["runLog"].([]string)
	d.DiffsA2, _ = res["diffsA2"].(int)
	d.Label, _ = res["label"].(string)

	// Cause statistics per finding code, for both pipelines.
	fs := catalogue()
	byCode := map[string]*finding{}
	for _, f := range fs {
		byCode[f.Code] = f
	}
	tally := func(rows []diffRow, a bool) {
		for _, r := range rows {
			for _, part := range strings.Split(r.Cause, " | ") {
				code := strings.SplitN(part, " ", 2)[0]
				f := byCode[code]
				if f == nil {
					continue
				}
				if a {
					f.RowsA++
					f.PtsA += r.SysPoints - r.ExpPoints
					f.RpA += r.SysCut - r.ExpCut
				} else {
					f.RowsB++
					f.PtsB += r.SysPoints - r.ExpPoints
					f.RpB += r.SysCut - r.ExpCut
				}
			}
		}
	}
	tally(diffsB, false)
	tally(diffsA, true)
	d.Findings = fs

	for _, r := range diffsB {
		if r.SysCut > r.ExpCut {
			d.WrongCutB += r.SysCut - r.ExpCut
		} else {
			d.MissedCutB += r.ExpCut - r.SysCut
		}
	}
	for _, r := range diffsA {
		if r.SysCut > r.ExpCut {
			d.WrongCutA += r.SysCut - r.ExpCut
		}
	}
	for _, e := range sums {
		if e.ScoreA == 0 {
			d.ZeroScoresA++
		}
	}

	// Planted-error catalogue: how many of each the final system got right.
	diffDays := map[string]bool{}
	for _, r := range diffsB {
		diffDays[r.Code+"|"+r.Date] = true
	}
	tagMap := map[string]*tagStat{}
	for _, t := range truth {
		for _, tg := range t.Tags {
			if tg != "random_late" {
				d.Planted++
				break
			}
		}
		for _, tg := range t.Tags {
			ts := tagMap[tg]
			if ts == nil {
				ts = &tagStat{Tag: tg}
				tagMap[tg] = ts
			}
			ts.Days++
			if !diffDays[t.Emp.DBCode+"|"+t.Date.Format("2006-01-02")] {
				ts.Correct++
			}
		}
	}
	for _, ts := range tagMap {
		d.Tags = append(d.Tags, *ts)
	}
	sort.Slice(d.Tags, func(i, j int) bool {
		ri := float64(d.Tags[i].Correct) / float64(d.Tags[i].Days)
		rj := float64(d.Tags[j].Correct) / float64(d.Tags[j].Days)
		if ri != rj {
			return ri < rj
		}
		return d.Tags[i].Tag < d.Tags[j].Tag
	})

	var offs []float64
	for _, x := range drift {
		offs = append(offs, x.MedianOffsetMin)
	}
	sort.Float64s(offs)
	d.DriftMedian = offs[len(offs)/2]
	for k, v := range unmatched {
		d.Unmatched = append(d.Unmatched, kv{k, v})
	}
	sort.Slice(d.Unmatched, func(i, j int) bool { return d.Unmatched[i].V > d.Unmatched[j].V })

	funcs := template.FuncMap{
		"rp": rp,
		"sev": func(s string) string {
			return strings.ToLower(strings.ReplaceAll(s, " ", "-"))
		},
		"pct": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a * 100 / b
		},
		"f1":    func(f float64) string { return fmt.Sprintf("%+.1f", f) },
		"drift": func(x driftDay, med float64) bool { return x.MedianOffsetMin-med >= 6 },
		"sub":   func(a, b int) int { return a - b },
		"sub64": func(a, b int64) int64 { return a - b },
		"short": func(s string) string { return strings.ReplaceAll(s, " | ", "\n") },
	}
	tpl, err := template.New("r").Funcs(funcs).Parse(reportHTML)
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(out, "report.html"))
	if err != nil {
		return err
	}
	defer f.Close()
	return tpl.Execute(f, d)
}

const reportHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Attendance Simulation October 2026</title>
<style>
:root{--bg:#f7f7f5;--card:#fff;--ink:#1d2129;--mute:#5d6470;--line:#e3e4e8;--accent:#2f6fd0;
--crit:#c0362c;--crit-bg:#fdecea;--high:#b8620b;--high-bg:#fdf1e3;--med:#7a5af0;--med-bg:#f1edfd;--rec:#0f7b6c;--rec-bg:#e5f5f2;--ok:#1f8a4c;--ok-bg:#e6f4ec}
@media (prefers-color-scheme:dark){:root:not([data-theme=light]){--bg:#15171b;--card:#1d2026;--ink:#e8e9ec;--mute:#9aa1ad;--line:#30343c;--accent:#78a6f0;
--crit:#f07a70;--crit-bg:#3a1f1d;--high:#f0a65a;--high-bg:#3a2a18;--med:#b3a0ff;--med-bg:#2a2440;--rec:#5cc9b6;--rec-bg:#16312d;--ok:#6bd394;--ok-bg:#173022}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.55 system-ui,-apple-system,"Segoe UI",sans-serif}
main{max-width:1180px;margin:0 auto;padding:32px 16px 64px}
h1{font-size:28px;margin:0 0 6px}h2{font-size:20px;margin:40px 0 12px}h3{font-size:16px;margin:0}
p.lede{color:var(--mute);margin:0 0 24px;max-width:860px}
.kpis{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:12px}
.kpi{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:14px}
.kpi b{display:block;font-size:24px;font-variant-numeric:tabular-nums}.kpi span{color:var(--mute);font-size:13px}
.kpi.bad b{color:var(--crit)}
.f{background:var(--card);border:1px solid var(--line);border-left:4px solid var(--line);border-radius:10px;padding:16px;margin:10px 0}
.f.critical{border-left-color:var(--crit)}.f.high{border-left-color:var(--high)}.f.medium{border-left-color:var(--med)}.f.record-error{border-left-color:var(--rec)}
.f header{display:flex;gap:10px;align-items:baseline;flex-wrap:wrap}
.tag{font-size:12px;font-weight:600;padding:2px 8px;border-radius:99px;white-space:nowrap}
.tag.critical{background:var(--crit-bg);color:var(--crit)}.tag.high{background:var(--high-bg);color:var(--high)}
.tag.medium{background:var(--med-bg);color:var(--med)}.tag.record-error{background:var(--rec-bg);color:var(--rec)}
.code{font:600 13px ui-monospace,Consolas,monospace;color:var(--mute)}
.f p{margin:8px 0}.f .meta{font-size:13px;color:var(--mute)}.f .meta code{font-size:12px}
.ev{display:flex;gap:16px;flex-wrap:wrap;font-size:13px;background:var(--bg);border-radius:8px;padding:8px 10px;margin-top:8px;font-variant-numeric:tabular-nums}
.ev b{font-weight:600}
.wrap{overflow-x:auto;background:var(--card);border:1px solid var(--line);border-radius:10px}
table{border-collapse:collapse;width:100%;font-size:13px;font-variant-numeric:tabular-nums}
th,td{padding:7px 10px;border-bottom:1px solid var(--line);text-align:left;vertical-align:top}
th{position:sticky;top:0;background:var(--card);font-weight:600;color:var(--mute);white-space:nowrap}
td.n,th.n{text-align:right;white-space:nowrap}tr:last-child td{border-bottom:0}
.bad{color:var(--crit);font-weight:600}.good{color:var(--ok);font-weight:600}
.bar{height:6px;border-radius:3px;background:var(--line);min-width:80px}.bar i{display:block;height:6px;border-radius:3px;background:var(--ok)}
.hl td{background:var(--crit-bg)}
.cols{display:grid;grid-template-columns:1fr 1fr;gap:16px}@media(max-width:800px){.cols{grid-template-columns:1fr}}
.note{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:12px 14px;margin:8px 0}
.note b{margin-right:6px}
code{font-family:ui-monospace,Consolas,monospace}
pre{white-space:pre-wrap;font-size:12px;color:var(--mute);margin:0}
.small{font-size:12px;color:var(--mute)}
</style></head><body><main>
<h1>Attendance simulation: October 2026</h1>
{{if .Label}}<p class="note" style="margin:6px 0 14px"><b>{{.Label}}</b></p>{{end}}
<p class="lede">{{.Employees}} simulated employees on the live Warung Jambu configuration (shifts OPN/MID/LONG/JMP, grace 5 min, early-leave tolerance 10 min, all live performance policies, absence grace 4 days).
{{.Punches}} fingerprint punches were generated as ZKTeco <code>attlog.dat</code> files with planted record errors on {{.Planted}} employee-days, then run through the app's real code: the fingerprint import handler, the nightly reconcile and evaluate job, the performance backfill, the manager-correction handlers and the payroll pay-cut calculation. The results are compared with an independent implementation of <i>Aturan Absen, Potongan Gaji &amp; Poin Kinerja</i>.</p>

<div class="kpis">
<div class="kpi"><b>{{.EmployeeDays}}</b><span>employee-days simulated</span></div>
<div class="kpi"><b>{{.Planted}}</b><span>employee-days with a planted error</span></div>
<div class="kpi bad"><b>{{.DiffsB}}</b><span>days wrong, best-case workflow (B)</span></div>
<div class="kpi bad"><b>{{.DiffsA}}</b><span>days wrong, weekly-import workflow (A)</span></div>
<div class="kpi"><b>{{.DiffsA2}}</b><span>days wrong in A after Penyesuaian Evaluasi</span></div>
<div class="kpi bad"><b>{{rp .WrongCutB}}</b><span>wrongful pay cuts (B)</span></div>
<div class="kpi bad"><b>{{.ZeroScoresA}} / {{.Employees}}</b><span>employees at score 0 in workflow A</span></div>
</div>

<p class="small" style="margin-top:12px"><b>Workflow A</b> = nightly job every night and the cumulative attlog.dat imported every Monday (how a file-export device is typically used). <b>Workflow B</b> = one import of the whole month, then the "re-evaluate" backfill, then manager corrections (the best case). Server timezone for this run: this PC (WIB). A second run as a UTC server is described under D2.</p>

<h2>System defects and record errors</h2>
<p class="small">Evidence = days on which the system disagrees with the rulebook because of this cause. Δ points and Δ Rp are system minus expected (positive = the employee was over-penalised).</p>
{{range .Findings}}
<div class="f {{sev .Severity}}"><header><span class="code">{{.Code}}</span><h3>{{.Title}}</h3><span class="tag {{sev .Severity}}">{{.Severity}}</span></header>
<p>{{.What}}</p>
<div class="ev"><span>Workflow B: <b>{{.RowsB}}</b> days · Δ {{.PtsB}} pts · Δ {{rp .RpB}}</span><span>Workflow A: <b>{{.RowsA}}</b> days · Δ {{.PtsA}} pts · Δ {{rp .RpA}}</span></div>
<p class="meta">Where: <code>{{.Where}}</code><br>Fix: {{.Fix}}</p></div>
{{end}}

<h2>How each planted error was handled (workflow B)</h2>
<div class="wrap"><table><thead><tr><th>Planted error</th><th class="n">Days</th><th class="n">Handled correctly</th><th>Rate</th></tr></thead><tbody>
{{range .Tags}}<tr><td><code>{{.Tag}}</code></td><td class="n">{{.Days}}</td><td class="n">{{.Correct}}</td><td><div class="bar"><i style="width:{{pct .Correct .Days}}%"></i></div></td></tr>{{end}}
</tbody></table></div>
<p class="small">Working as intended: late tiers 6/11/21/31/75 min and the within-grace case, early leave in and out of tolerance, missing check-out/check-in flags, scattered absences with the 4-day grace, 2-day streak penalties, no-punch correction, both half-day corrections (lost minutes 190 and 480), double taps (de-duplicated), stray IDs reported as unmatched, and the 15-minute round-up with a one-day cap.</p>

<h2>Per employee</h2>
<div class="wrap"><table><thead><tr><th>Code</th><th>Name</th><th>Scenario</th><th class="n">Score expected</th><th class="n">Score B</th><th class="n">Score A</th><th class="n">Score A after adjustment</th><th class="n">Cut expected</th><th class="n">Cut B</th><th class="n">Cut A</th><th class="n">Days wrong (B)</th></tr></thead><tbody>
{{range .Emps}}<tr><td><code>{{.Code}}</code></td><td>{{.Name}}</td><td class="small">{{.Story}}</td>
<td class="n">{{.ExpScore}}</td><td class="n {{if ne .ScoreB .ExpScore}}bad{{end}}">{{.ScoreB}}</td><td class="n {{if ne .ScoreA .ExpScore}}bad{{end}}">{{.ScoreA}}</td><td class="n {{if ne .ScoreA2 .ExpScore}}bad{{end}}">{{.ScoreA2}}</td>
<td class="n">{{rp .ExpCut}}</td><td class="n {{if ne .CutB .ExpCut}}bad{{end}}">{{rp .CutB}}</td><td class="n {{if ne .CutA .ExpCut}}bad{{end}}">{{rp .CutA}}</td><td class="n">{{.Diffs}}</td></tr>{{end}}
</tbody></table></div>
<p class="small">Cut = late/early pay cut only. Half-day deductions are compared separately and matched in every case.</p>

<div class="cols">
<div><h2>Device clock check</h2>
<p class="small">Median minutes between the first punch and the shift start, per day (month median {{f1 .DriftMedian}}). Days ≥ 6 min above it are highlighted.</p>
<div class="wrap" style="max-height:360px"><table><thead><tr><th>Date</th><th class="n">Present</th><th class="n">Marked late</th><th class="n">Median offset</th></tr></thead><tbody>
{{$m := .DriftMedian}}{{range .Drift}}<tr {{if drift . $m}}class="hl"{{end}}><td>{{.Date}}</td><td class="n">{{.Present}}</td><td class="n">{{.Late}}</td><td class="n">{{f1 .MedianOffsetMin}}</td></tr>{{end}}
</tbody></table></div></div>
<div><h2>Unmatched device IDs</h2>
<p class="small">Punches in the file whose user ID matches no employee_code. These are silently skipped at confirm.</p>
<div class="wrap"><table><thead><tr><th>Device user ID</th><th class="n">Punches dropped</th></tr></thead><tbody>
{{range .Unmatched}}<tr><td><code>{{.K}}</code></td><td class="n">{{.V}}</td></tr>{{end}}
</tbody></table></div>
<h2>Config vs rulebook</h2>
{{range .Gaps}}<div class="note"><b>{{.Code}}</b><b>{{.Title}}.</b> {{.Detail}}</div>{{end}}
</div></div>

<h2>Other issues found while reading the code (not simulated)</h2>
{{range .Notes}}<div class="note"><b>{{.Code}}</b><b>{{.Title}}.</b> {{.Detail}}</div>{{end}}

<h2>Every wrong day, workflow B ({{.DiffsB}})</h2>
<div class="wrap" style="max-height:640px"><table><thead><tr><th>Code</th><th>Date</th><th>Expected</th><th>System</th><th class="n">Pts exp/sys</th><th class="n">Cut exp</th><th class="n">Cut sys</th><th>Cause</th></tr></thead><tbody>
{{range .Diffs}}<tr><td><code>{{.Code}}</code></td><td>{{.Date}}</td><td>{{.ExpStatus}} {{.ExpRules}}</td><td>{{.SysStatus}} {{.SysRules}}</td>
<td class="n">{{.ExpPoints}} / {{.SysPoints}}</td><td class="n">{{rp .ExpCut}}</td><td class="n">{{rp .SysCut}}</td><td><pre>{{short .Cause}}</pre></td></tr>{{end}}
</tbody></table></div>
<p class="small">Workflow A's {{.DiffsA}} wrong days are in <code>3_differences_A_weekly.csv</code>.</p>

<h2>Files in this folder</h2>
<div class="wrap"><table><tbody>
<tr><td><code>device-files/*.dat</code></td><td>The simulated ZKTeco exports: weekly cumulative files (workflow A) and the full month (workflow B). They can be imported into the app.</td></tr>
<tr><td><code>1_ground_truth.csv</code></td><td>What really happened each employee-day, what the device recorded, the planted errors, and the expected status, points and cut per the rulebook.</td></tr>
<tr><td><code>2_system_records_*.csv</code></td><td>attendance_records as the app stored them, showing the device-clock time next to what the app displays.</td></tr>
<tr><td><code>3_differences_*.csv</code></td><td>Every employee-day where the system and the rulebook disagree, with the cause.</td></tr>
<tr><td><code>4_employee_summary.csv</code></td><td>Scores and pay cuts per employee: expected, workflow A and workflow B.</td></tr>
<tr><td><code>results.json</code>, <code>run_log.txt</code></td><td>Raw results and the import and correction log.</td></tr>
<tr><td>Databases</td><td><code>attendance_sim</code> (B) and <code>attendance_sim_weekly</code> (A) on your local Postgres. Point the app's DB_NAME at one to browse it in the UI.</td></tr>
<tr><td>Re-run</td><td><code>cd server-go &amp;&amp; go run ./cmd/sim-attendance-month</code> (add <code>-server-tz UTC</code> to emulate a UTC server)</td></tr>
</tbody></table></div>
<h2>Run log</h2><div class="note"><pre>{{range .RunLog}}{{.}}
{{end}}</pre></div>
</main></body></html>`
