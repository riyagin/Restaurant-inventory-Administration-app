// sim-attendance-month simulates one month (October 2026) of fingerprint
// attendance for 20 employees, plants a mix of attendance record errors, runs the
// punches through the app's REAL pipeline (FingerprintConfirm handler, nightly
// ReconcileAbsent + EvaluateDay, the performance backfill, the manager
// correction handlers and the payroll pay-cut calculation) and compares the
// outcome against an independent implementation of the rulebook.
//
// It never touches the dev database: it builds two throwaway databases,
// attendance_sim_weekly (pipeline A) and attendance_sim (pipeline B), and writes
// every artefact to the output folder.
//
// Usage (from server-go/):
//
//	go run ./cmd/sim-attendance-month -out "../../attendance-simulation-2026-10"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"inventory-app/server-go/internal/config"
)

func main() {
	out := flag.String("out", "../../attendance-simulation-2026-10", "output folder")
	serverTZ := flag.String("server-tz", "", "pretend the API server runs in this zone (e.g. UTC, like a typical VPS); default = this machine")
	label := flag.String("label", "", "note shown at the top of the report (e.g. which code version ran)")
	flag.Parse()
	if *serverTZ != "" {
		loc, err := time.LoadLocation(*serverTZ)
		if err != nil {
			log.Fatal(err)
		}
		time.Local = loc // pgx decodes timestamptz into time.Local
	}
	fmt.Println("server time zone:", time.Now().Location())
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	must := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(*out, "device-files"), 0o755))

	rng := rand.New(rand.NewPCG(2026, 10))
	emps, truth, punches := buildTruth(rng)
	computeExpected(truth)
	fmt.Printf("Simulation: %d employees, %d employee-days, %d punches\n", len(emps), len(truth), len(punches))

	nextMonth := day(daysInMonth()).AddDate(0, 0, 1)
	full := formatAttlog(punches, nextMonth)
	must(os.WriteFile(filepath.Join(*out, "device-files", "attlog_2026-10_full.dat"), full, 0o644))

	// ── Pipeline A: nightly job every night, cumulative .dat imported Mondays ──
	fmt.Println("\n[A] nightly reconcile+evaluate, cumulative attlog imported every Monday")
	urlA, err := recreateSimDB(ctx, cfg, SimDBWeekly)
	must(err)
	a, err := newSim(ctx, urlA)
	must(err)
	must(a.seed(emps, truth))
	must(a.grantPermits(truth))
	for d := day(2); !d.After(nextMonth.AddDate(0, 0, 1)); d = d.AddDate(0, 0, 1) {
		must(a.nightly(d.AddDate(0, 0, -1)))
		if d.Weekday() == time.Monday {
			name := "attlog_" + d.Format("2006-01-02") + ".dat"
			data := formatAttlog(punches, d)
			must(os.WriteFile(filepath.Join(*out, "device-files", name), data, 0o644))
			must(a.importDat(name, data))
		}
	}
	snapA, err := a.snapshot(emps)
	must(err)
	// HR then opens Penyesuaian Evaluasi for the month and applies it.
	_, err = a.adjust(day(1).Format("2006-01"))
	must(err)
	snapA2, err := a.snapshot(emps)
	must(err)

	// ── Pipeline B: one full import, re-evaluate, manager corrections ─────────
	fmt.Println("\n[B] one full import -> re-evaluate (backfill) -> manager corrections")
	urlB, err := recreateSimDB(ctx, cfg, SimDBFinal)
	must(err)
	b, err := newSim(ctx, urlB)
	must(err)
	must(b.seed(emps, truth))
	must(b.grantPermits(truth))
	must(b.importDat("attlog_2026-10_full.dat", full))
	must(b.backfill(day(1), day(daysInMonth())))
	snapPre, err := b.snapshot(emps)
	must(err)
	for _, t := range truth {
		if t.Corr != nil {
			must(b.applyCorrection(t))
		}
	}
	snapB, err := b.snapshot(emps)
	must(err)

	must(writeOutputs(*out, emps, truth, punches, snapA, snapA2, snapPre, snapB, append(a.log, b.log...), *label))
	fmt.Println("\nDone ->", *out)
}
