package main

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// runDev loads the October simulation into the app database: one full .dat
// import, Dinas Luar / izin permits, the month's evaluation and the manager
// corrections, all through the real handlers and services.
func runDev(ctx context.Context, url, out string, cleanOnly bool, label string) {
	must := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	s, err := newSim(ctx, url)
	must(err)
	s.devMode = true
	n, err := cleanDev(ctx, s)
	must(err)
	fmt.Printf("removed %d SIM employees and their data from the app database\n", n)
	if cleanOnly {
		return
	}

	rng := rand.New(rand.NewPCG(2026, 10))
	emps, truth, punches := buildTruth(rng)
	computeExpected(truth)
	must(checkDevCodes(ctx, s, emps))
	fmt.Printf("Simulation: %d employees, %d employee-days, %d punches -> app database\n", len(emps), len(truth), len(punches))

	must(os.MkdirAll(filepath.Join(out, "device-files"), 0o755))
	nextMonth := day(daysInMonth()).AddDate(0, 0, 1)
	full := formatAttlog(punches, nextMonth)
	must(os.WriteFile(filepath.Join(out, "device-files", "attlog_2026-10_full.dat"), full, 0o644))

	must(s.seed(emps, truth))
	must(s.grantPermits(truth))
	must(s.importDat("attlog_2026-10_full.dat", full))
	must(s.backfill(day(1), day(daysInMonth())))
	pre, err := s.snapshot(emps)
	must(err)
	for _, t := range truth {
		if t.Corr != nil {
			must(s.applyCorrection(t))
		}
	}
	post, err := s.snapshot(emps)
	must(err)
	// The report has two workflows; in -dev mode both columns show this run.
	must(writeOutputs(out, emps, truth, punches, pre, post, pre, post, s.log, label))
	fmt.Println("\nDone ->", out)
	fmt.Printf("Open the app: Kinerja / Absensi, branch %q, October 2026.\n", simBranchName)
}
