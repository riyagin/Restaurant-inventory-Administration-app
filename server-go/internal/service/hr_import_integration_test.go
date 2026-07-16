package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
	"inventory-app/server-go/internal/testutil"
)

// hrImportFixtures holds everything a re-upload/update test needs: two
// distinct positions/branches (so the row can move the employee between
// them), three wage components (one per type), a starting employee, and its
// initial wage structure.
type hrImportFixtures struct {
	posA, posB       *db.Position
	branchA, branchB *db.Branch
	allowance        *db.WageComponent
	bonus            *db.WageComponent
	deduction        *db.WageComponent
	employeeID       pgtype.UUID
	employeeCode     string
	userID           pgtype.UUID
}

func setupHRImportFixtures(t *testing.T, ctx context.Context, qtx *db.Queries) hrImportFixtures {
	t.Helper()
	suffix := uuid.New().String()[:8]

	mkAccount := func(name, typ string) pgtype.UUID {
		a, err := qtx.CreateAccount(ctx, &db.CreateAccountParams{Name: name + "-" + suffix, AccountType: typ})
		if err != nil {
			t.Fatalf("create account %s: %v", name, err)
		}
		return a.ID
	}

	posA, err := qtx.CreatePosition(ctx, &db.CreatePositionParams{Name: "PosA-" + suffix, IsActive: true})
	if err != nil {
		t.Fatalf("create posA: %v", err)
	}
	posB, err := qtx.CreatePosition(ctx, &db.CreatePositionParams{Name: "PosB-" + suffix, IsActive: true})
	if err != nil {
		t.Fatalf("create posB: %v", err)
	}

	branchA, err := qtx.CreateBranch(ctx, &db.CreateBranchParams{
		Name: "BranchA-" + suffix, RevenueAccountID: mkAccount("BrARev", "revenue"), ExpenseAccountID: mkAccount("BrAExp", "expense"),
	})
	if err != nil {
		t.Fatalf("create branchA: %v", err)
	}
	branchB, err := qtx.CreateBranch(ctx, &db.CreateBranchParams{
		Name: "BranchB-" + suffix, RevenueAccountID: mkAccount("BrBRev", "revenue"), ExpenseAccountID: mkAccount("BrBExp", "expense"),
	})
	if err != nil {
		t.Fatalf("create branchB: %v", err)
	}

	allowance, err := qtx.CreateWageComponent(ctx, &db.CreateWageComponentParams{
		Name: "Allow-" + suffix, Type: "allowance", IsFixed: true, IsActive: true, CalcMethod: "fixed",
	})
	if err != nil {
		t.Fatalf("create allowance component: %v", err)
	}
	bonus, err := qtx.CreateWageComponent(ctx, &db.CreateWageComponentParams{
		Name: "Bonus-" + suffix, Type: "bonus", IsFixed: true, IsActive: true, CalcMethod: "fixed",
	})
	if err != nil {
		t.Fatalf("create bonus component: %v", err)
	}
	deduction, err := qtx.CreateWageComponent(ctx, &db.CreateWageComponentParams{
		Name: "Deduct-" + suffix, Type: "deduction", IsFixed: true, IsActive: true, CalcMethod: "fixed",
	})
	if err != nil {
		t.Fatalf("create deduction component: %v", err)
	}

	userID := pgtype.UUID{}
	code := "TEST-" + suffix
	empID, err := qtx.CreateEmployee(ctx, &db.CreateEmployeeParams{
		EmployeeCode:      code,
		FullName:          "Old Name",
		Dob:               pgtype.Date{Time: mustDate("1990-01-01"), Valid: true},
		JoinDate:          pgtype.Date{Time: mustDate("2020-01-01"), Valid: true},
		PositionID:        posA.ID,
		BranchID:          branchA.ID,
		Phone:             pgtype.Text{String: "0810000000", Valid: true},
		Email:             pgtype.Text{String: "old@example.id", Valid: true},
		Address:           pgtype.Text{String: "Old Address", Valid: true},
		NationalID:        pgtype.Text{String: "1111111111111111", Valid: true},
		BankName:          pgtype.Text{String: "BankOld", Valid: true},
		BankAccountNumber: pgtype.Text{String: "1111", Valid: true},
		BankAccountHolder: pgtype.Text{String: "Old Name", Valid: true},
		UserID:            userID,
		Status:            "inactive",
		EmploymentType:    "contract",
		ContractEndDate:   pgtype.Date{Time: mustDate("2026-12-31"), Valid: true},
	})
	if err != nil {
		t.Fatalf("create employee: %v", err)
	}

	if _, err := service.CreateWageVersion(ctx, qtx, service.CreateWageVersionParams{
		EmployeeID:    empID,
		BaseSalary:    5000000,
		WorkingDays:   25,
		EffectiveDate: mustDate("2024-01-01"),
		CreatedBy:     pgtype.UUID{},
		Components: []service.WageComponentInput{
			{ComponentID: allowance.ID, Amount: 100000},
		},
	}); err != nil {
		t.Fatalf("create initial wage version: %v", err)
	}

	return hrImportFixtures{
		posA: posA, posB: posB,
		branchA: branchA, branchB: branchB,
		allowance: allowance, bonus: bonus, deduction: deduction,
		employeeID:   empID,
		employeeCode: code,
		userID:       userID,
	}
}

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// buildRef assembles the HRImportRefData a real Confirm request would build
// via handler.loadRefData, scoped to just this test's fixtures plus the
// standard existing-code/name-dob snapshot needed for update detection.
func buildRef(f hrImportFixtures, hasWage bool) service.HRImportRefData {
	ref := service.HRImportRefData{
		Positions: map[string]pgtype.UUID{
			strings.ToLower(f.posA.Name): f.posA.ID,
			strings.ToLower(f.posB.Name): f.posB.ID,
		},
		Branches: map[string]pgtype.UUID{
			strings.ToLower(f.branchA.Name): f.branchA.ID,
			strings.ToLower(f.branchB.Name): f.branchB.ID,
		},
		ExistingByCode: map[string]*service.ExistingEmployeeSnapshot{
			strings.ToLower(f.employeeCode): {
				ID:                 f.employeeID,
				EmploymentType:     "contract",
				ContractEndDate:    pgtype.Date{Time: mustDate("2026-12-31"), Valid: true},
				UserID:             f.userID,
				Status:             "inactive",
				HasWage:            hasWage,
				CurrentBaseSalary:  5000000,
				CurrentWorkingDays: 25,
				CurrentEffDate:     "2024-01-01",
				CurrentComponents:  map[string]int64{strings.ToLower(f.allowance.Name): 100000},
			},
		},
		ExistingNameDob: map[string]bool{},
		Components: map[string]*db.WageComponent{
			f.allowance.Name: f.allowance,
			f.bonus.Name:     f.bonus,
			f.deduction.Name: f.deduction,
		},
		MaxEmployeeCodeSeq: 0,
	}
	return ref
}

// componentColsFor returns the ordered component column list matching how
// the real template/export sorts them: by (type, name) — allowance, bonus,
// deduction alphabetically within type.
func componentColsFor(f hrImportFixtures) []*db.WageComponent {
	return []*db.WageComponent{f.allowance, f.bonus, f.deduction}
}

// TestConfirmHRImport_UpdateAllFields re-uploads a row for an existing
// employee with every editable field changed (master data + position/branch
// move + wage amounts + a newly-added component), and verifies each field
// actually persisted, while fields the template doesn't carry (status,
// employment_type, contract_end_date, user_id) are preserved untouched.
func TestConfirmHRImport_UpdateAllFields(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx, _, qtx := testutil.OpenTx(t, pool)
	f := setupHRImportFixtures(t, ctx, qtx)
	ref := buildRef(f, true)

	hdr := append(append([]string{}, service.HRImportFixedHeaders...),
		service.ComponentColumnHeader(f.allowance),
		service.ComponentColumnHeader(f.bonus),
		service.ComponentColumnHeader(f.deduction))

	row := []string{
		f.employeeCode, "New Name", "1991-02-02", "2021-03-03",
		f.posB.Name, f.branchB.Name,
		"0829999999", "new@example.id", "New Address", "2222222222222222",
		"BankNew", "9999", "New Name",
		"6000000", "26", "2024-02-01",
		"150000", "50000", "", // allowance changed, bonus added, deduction left blank
	}

	preview := service.ParseHRImportRows("edited-export.xlsx", hdr, [][]string{row}, componentColsFor(f), ref)
	if preview.ErrorCount != 0 {
		t.Fatalf("expected 0 errors, got %d: %+v", preview.ErrorCount, preview.Rows[0].Messages)
	}
	r := preview.Rows[0]
	if r.Action != "update" {
		t.Fatalf("action = %s, want update", r.Action)
	}
	if !r.WageChanged {
		t.Fatalf("expected WageChanged = true (base salary/working days/components all differ)")
	}
	if preview.UpdateCount != 1 || preview.CreateCount != 0 {
		t.Fatalf("update_count=%d create_count=%d, want 1/0", preview.UpdateCount, preview.CreateCount)
	}

	result, err := service.ConfirmHRImport(ctx, qtx, preview, ref, pgtype.UUID{})
	if err != nil {
		t.Fatalf("ConfirmHRImport: %v", err)
	}
	if result.EmployeesUpdated != 1 || result.EmployeesCreated != 0 {
		t.Fatalf("updated=%d created=%d, want 1/0", result.EmployeesUpdated, result.EmployeesCreated)
	}

	// ── Verify every employee master field updated ──
	emp, err := qtx.GetEmployeeByID(ctx, f.employeeID)
	if err != nil {
		t.Fatalf("GetEmployeeByID: %v", err)
	}
	checks := []struct {
		field, got, want string
	}{
		{"full_name", emp.FullName, "New Name"},
		{"dob", emp.Dob.Time.Format("2006-01-02"), "1991-02-02"},
		{"join_date", emp.JoinDate.Time.Format("2006-01-02"), "2021-03-03"},
		{"position_name", emp.PositionName, f.posB.Name},
		{"branch_name", emp.BranchName, f.branchB.Name},
		{"phone", emp.Phone.String, "0829999999"},
		{"email", emp.Email.String, "new@example.id"},
		{"address", emp.Address.String, "New Address"},
		{"national_id", emp.NationalID.String, "2222222222222222"},
		{"bank_name", emp.BankName.String, "BankNew"},
		{"bank_account_number", emp.BankAccountNumber.String, "9999"},
		{"bank_account_holder", emp.BankAccountHolder.String, "New Name"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}

	// ── Fields NOT in the template must be preserved from the pre-import row ──
	if emp.Status != "inactive" {
		t.Errorf("status = %q, want preserved 'inactive'", emp.Status)
	}
	if emp.EmploymentType != "contract" {
		t.Errorf("employment_type = %q, want preserved 'contract'", emp.EmploymentType)
	}
	if !emp.ContractEndDate.Valid || emp.ContractEndDate.Time.Format("2006-01-02") != "2026-12-31" {
		t.Errorf("contract_end_date = %+v, want preserved 2026-12-31", emp.ContractEndDate)
	}

	// ── Verify the wage structure versioned correctly ──
	cur, err := qtx.GetCurrentOpenWageStructure(ctx, f.employeeID)
	if err != nil {
		t.Fatalf("GetCurrentOpenWageStructure: %v", err)
	}
	if cur.BaseSalary != 6000000 {
		t.Errorf("base_salary = %d, want 6000000", cur.BaseSalary)
	}
	if cur.WorkingDaysPerMonth != 26 {
		t.Errorf("working_days_per_month = %d, want 26", cur.WorkingDaysPerMonth)
	}
	if cur.EffectiveDate.Time.Format("2006-01-02") != "2024-02-01" {
		t.Errorf("effective_date = %s, want 2024-02-01", cur.EffectiveDate.Time.Format("2006-01-02"))
	}

	comps, err := qtx.ListEmployeeWageComponents(ctx, cur.ID)
	if err != nil {
		t.Fatalf("ListEmployeeWageComponents: %v", err)
	}
	gotAmounts := map[string]int64{}
	for _, c := range comps {
		gotAmounts[c.ComponentName] = c.Amount
	}
	if gotAmounts[f.allowance.Name] != 150000 {
		t.Errorf("allowance amount = %d, want 150000", gotAmounts[f.allowance.Name])
	}
	if gotAmounts[f.bonus.Name] != 50000 {
		t.Errorf("bonus amount = %d, want 50000 (newly attached)", gotAmounts[f.bonus.Name])
	}
	if _, ok := gotAmounts[f.deduction.Name]; ok {
		t.Errorf("deduction should be unattached (blank cell), got %d", gotAmounts[f.deduction.Name])
	}

	// The old wage_structures row must be closed, not left dangling open.
	history, err := qtx.ListWageStructuresByEmployee(ctx, f.employeeID)
	if err != nil {
		t.Fatalf("ListWageStructuresByEmployee: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 wage_structures rows (old closed + new open), got %d", len(history))
	}
	openCount := 0
	for _, h := range history {
		if !h.EndDate.Valid {
			openCount++
		}
	}
	if openCount != 1 {
		t.Errorf("expected exactly 1 open wage_structures row, got %d", openCount)
	}
}

// TestConfirmHRImport_UnchangedWageSkipsNewVersion re-uploads a row for an
// existing employee whose master data changed but wage data is byte-for-byte
// identical to the current version — the expected behavior (matching what a
// re-exported-then-lightly-edited file would produce) is that the employee
// updates but NO new wage_structures version is created.
func TestConfirmHRImport_UnchangedWageSkipsNewVersion(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx, _, qtx := testutil.OpenTx(t, pool)
	f := setupHRImportFixtures(t, ctx, qtx)
	ref := buildRef(f, true)

	hdr := append(append([]string{}, service.HRImportFixedHeaders...),
		service.ComponentColumnHeader(f.allowance),
		service.ComponentColumnHeader(f.bonus),
		service.ComponentColumnHeader(f.deduction))

	// Only full_name changes; wage fields match the fixture's current version
	// exactly (base 5000000 / 25 days / effective 2024-01-01 / allowance 100000).
	row := []string{
		f.employeeCode, "Renamed Only", "1990-01-01", "2020-01-01",
		f.posA.Name, f.branchA.Name,
		"0810000000", "old@example.id", "Old Address", "1111111111111111",
		"BankOld", "1111", "Old Name",
		"5000000", "25", "2024-01-01",
		"100000", "", "",
	}

	preview := service.ParseHRImportRows("edited-export.xlsx", hdr, [][]string{row}, componentColsFor(f), ref)
	if preview.ErrorCount != 0 {
		t.Fatalf("expected 0 errors, got %d: %+v", preview.ErrorCount, preview.Rows[0].Messages)
	}
	if preview.Rows[0].WageChanged {
		t.Fatalf("expected WageChanged = false when wage data is unchanged")
	}

	result, err := service.ConfirmHRImport(ctx, qtx, preview, ref, pgtype.UUID{})
	if err != nil {
		t.Fatalf("ConfirmHRImport: %v", err)
	}
	if result.EmployeesUpdated != 1 {
		t.Fatalf("updated=%d, want 1", result.EmployeesUpdated)
	}

	emp, err := qtx.GetEmployeeByID(ctx, f.employeeID)
	if err != nil {
		t.Fatalf("GetEmployeeByID: %v", err)
	}
	if emp.FullName != "Renamed Only" {
		t.Errorf("full_name = %q, want 'Renamed Only'", emp.FullName)
	}

	history, err := qtx.ListWageStructuresByEmployee(ctx, f.employeeID)
	if err != nil {
		t.Fatalf("ListWageStructuresByEmployee: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected wage history untouched (1 row), got %d", len(history))
	}
	if history[0].EndDate.Valid {
		t.Errorf("the single wage_structures row should still be open, got end_date = %+v", history[0].EndDate)
	}
}

// TestConfirmHRImport_WageChangeRequiresLaterEffectiveDate verifies that a
// row changing wage amounts but NOT advancing effective_date past the
// employee's current version is rejected with a clear error rather than
// silently accepted or crashing.
func TestConfirmHRImport_WageChangeRequiresLaterEffectiveDate(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx, _, qtx := testutil.OpenTx(t, pool)
	f := setupHRImportFixtures(t, ctx, qtx)
	ref := buildRef(f, true)

	hdr := append(append([]string{}, service.HRImportFixedHeaders...),
		service.ComponentColumnHeader(f.allowance),
		service.ComponentColumnHeader(f.bonus),
		service.ComponentColumnHeader(f.deduction))

	row := []string{
		f.employeeCode, "Old Name", "1990-01-01", "2020-01-01",
		f.posA.Name, f.branchA.Name,
		"0810000000", "old@example.id", "Old Address", "1111111111111111",
		"BankOld", "1111", "Old Name",
		"7000000", "25", "2024-01-01", // base salary changed but effective_date NOT advanced
		"100000", "", "",
	}

	preview := service.ParseHRImportRows("edited-export.xlsx", hdr, [][]string{row}, componentColsFor(f), ref)
	if preview.ErrorCount != 1 {
		t.Fatalf("expected 1 error row (effective date not advanced), got %d", preview.ErrorCount)
	}
	if preview.Rows[0].Status != "error" {
		t.Errorf("status = %s, want error", preview.Rows[0].Status)
	}
}
