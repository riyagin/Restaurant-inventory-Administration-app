// seed-hr inserts a minimal dev dataset for the HR module:
//   - 2 positions (Kasir, Koki)
//   - 3 employees across 2 branches
//   - 1 wage component (tunjangan makan, fixed Rp 200.000)
//   - wage structures for each employee (base ~Rp 3.000.000 + allowance)
//   - work schedule Mon–Sat 08:00–17:00 with 15-min late grace
//   - 2 public holidays for the current year
//   - 1 performance policy (late_arrival, 15 min threshold, deduction 1.0)
//
// Usage: go run ./server-go/cmd/seed-hr
// Reads DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASSWORD from server-go/.env
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DBUrl)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	year := time.Now().Year()

	// ── Fetch first two branches ──────────────────────────────────────────────
	branchRows, err := pool.Query(ctx, `SELECT id, name FROM branches ORDER BY name LIMIT 2`)
	if err != nil {
		log.Fatalf("failed to fetch branches: %v", err)
	}
	type branchRow struct {
		id   string
		name string
	}
	var branches []branchRow
	for branchRows.Next() {
		var b branchRow
		if err := branchRows.Scan(&b.id, &b.name); err != nil {
			log.Fatalf("failed to scan branch: %v", err)
		}
		branches = append(branches, b)
	}
	branchRows.Close()
	if len(branches) < 2 {
		log.Fatalf("seed requires at least 2 branches in the database; found %d. Add branches first.", len(branches))
	}
	fmt.Printf("Using branches: %s, %s\n", branches[0].name, branches[1].name)

	// ── Positions ─────────────────────────────────────────────────────────────
	var posKasirID, posKokiID string
	err = pool.QueryRow(ctx,
		`INSERT INTO positions (name, is_active)
		 VALUES ('Kasir', true)
		 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`).Scan(&posKasirID)
	if err != nil {
		log.Fatalf("failed to upsert position Kasir: %v", err)
	}
	fmt.Printf("Position Kasir: %s\n", posKasirID)

	err = pool.QueryRow(ctx,
		`INSERT INTO positions (name, is_active)
		 VALUES ('Koki', true)
		 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`).Scan(&posKokiID)
	if err != nil {
		log.Fatalf("failed to upsert position Koki: %v", err)
	}
	fmt.Printf("Position Koki: %s\n", posKokiID)

	// ── Wage component ────────────────────────────────────────────────────────
	var wageCompID string
	err = pool.QueryRow(ctx,
		`INSERT INTO wage_components (name, type, is_fixed, is_active)
		 VALUES ('Tunjangan Makan', 'allowance', true, true)
		 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`).Scan(&wageCompID)
	if err != nil {
		log.Fatalf("failed to upsert wage component: %v", err)
	}
	fmt.Printf("Wage component Tunjangan Makan: %s\n", wageCompID)

	// ── Employees ─────────────────────────────────────────────────────────────
	type empSeed struct {
		code     string
		name     string
		position string
		branch   string
		joinDate string
	}
	seeds := []empSeed{
		{"EMP001", "Budi Santoso", posKasirID, branches[0].id, fmt.Sprintf("%d-01-15", year-1)},
		{"EMP002", "Siti Rahayu", posKokiID, branches[0].id, fmt.Sprintf("%d-03-01", year-1)},
		{"EMP003", "Ahmad Fauzi", posKasirID, branches[1].id, fmt.Sprintf("%d-06-01", year)},
	}

	var empIDs []string
	for _, s := range seeds {
		var empID string
		err = pool.QueryRow(ctx,
			`INSERT INTO employees (employee_code, full_name, join_date, position_id, branch_id, status)
			 VALUES ($1, $2, $3::date, $4::uuid, $5::uuid, 'active')
			 ON CONFLICT (employee_code) DO UPDATE SET full_name = EXCLUDED.full_name
			 RETURNING id`,
			s.code, s.name, s.joinDate, s.position, s.branch,
		).Scan(&empID)
		if err != nil {
			log.Fatalf("failed to upsert employee %s: %v", s.name, err)
		}
		empIDs = append(empIDs, empID)
		fmt.Printf("Employee %s (%s): %s\n", s.name, s.code, empID)
	}

	// ── Wage structures ───────────────────────────────────────────────────────
	baseSalaries := []int64{3000000, 3500000, 2800000}
	today := time.Now().Format("2006-01-02")
	for i, empID := range empIDs {
		base := baseSalaries[i]
		dailyRate := base / 26

		var structID string
		err = pool.QueryRow(ctx,
			`INSERT INTO employee_wage_structures (employee_id, base_salary, daily_rate, working_days_per_month, effective_date)
			 VALUES ($1::uuid, $2, $3, 26, $4::date)
			 ON CONFLICT DO NOTHING
			 RETURNING id`,
			empID, base, dailyRate, today,
		).Scan(&structID)
		if err != nil {
			fmt.Printf("Wage structure for employee %s: already exists, skipping\n", empID)
			continue
		}

		_, err = pool.Exec(ctx,
			`INSERT INTO employee_wage_components (wage_structure_id, wage_component_id, amount)
			 VALUES ($1::uuid, $2::uuid, $3)
			 ON CONFLICT DO NOTHING`,
			structID, wageCompID, 200000,
		)
		if err != nil {
			log.Fatalf("failed to insert wage component for structure %s: %v", structID, err)
		}
		fmt.Printf("Wage structure for employee %s: base Rp %d, daily Rp %d\n", empID, base, dailyRate)
	}

	// ── Work schedule Mon–Sat 08:00–17:00, 15-min late grace ─────────────────
	workDays := []int{1, 2, 3, 4, 5, 6} // Mon=1 … Sat=6
	for _, day := range workDays {
		_, err = pool.Exec(ctx,
			`INSERT INTO work_schedules (day_of_week, start_time, end_time, late_grace_minutes)
			 VALUES ($1, '08:00', '17:00', 15)
			 ON CONFLICT (day_of_week) DO UPDATE
			   SET start_time = EXCLUDED.start_time,
			       end_time = EXCLUDED.end_time,
			       late_grace_minutes = EXCLUDED.late_grace_minutes`,
			day,
		)
		if err != nil {
			log.Fatalf("failed to upsert work schedule day %d: %v", day, err)
		}
	}
	fmt.Println("Work schedule Mon–Sat 08:00–17:00 (grace 15 min) upserted")

	// ── Public holidays ───────────────────────────────────────────────────────
	type holiday struct {
		date string
		name string
	}
	holidays := []holiday{
		{fmt.Sprintf("%d-08-17", year), "Hari Kemerdekaan RI"},
		{fmt.Sprintf("%d-01-01", year), "Tahun Baru Masehi"},
	}
	for _, h := range holidays {
		_, err = pool.Exec(ctx,
			`INSERT INTO public_holidays (date, name)
			 VALUES ($1::date, $2)
			 ON CONFLICT (date) DO UPDATE SET name = EXCLUDED.name`,
			h.date, h.name,
		)
		if err != nil {
			log.Fatalf("failed to upsert holiday %s: %v", h.name, err)
		}
		fmt.Printf("Holiday: %s (%s)\n", h.name, h.date)
	}

	// ── Performance policy ────────────────────────────────────────────────────
	var policyID string
	err = pool.QueryRow(ctx,
		`INSERT INTO performance_policies (name, violation_type, threshold_minutes, deduction_points, is_active)
		 VALUES ('Keterlambatan', 'late_arrival', 15, 1.0, true)
		 ON CONFLICT DO NOTHING
		 RETURNING id`,
	).Scan(&policyID)
	if err != nil {
		fmt.Printf("Performance policy already exists, skipping\n")
	} else {
		fmt.Printf("Performance policy Keterlambatan: %s\n", policyID)
	}

	// ── Summary ───────────────────────────────────────────────────────────────
	var empCount int64
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM employees`).Scan(&empCount)
	fmt.Printf("\nSeed complete. Total employees in DB: %d\n", empCount)
}
