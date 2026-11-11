package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

func TestDailyAllowanceItems(t *testing.T) {
	comps := []*db.ListEmployeeWageComponentsRow{
		{ComponentName: "Uang Makan", ComponentType: ComponentTypeDailyAllowance, ComponentCalcMethod: CalcMethodPerPresentDay, Amount: 25000},
		{ComponentName: "Uang Transport", ComponentType: ComponentTypeDailyAllowance, ComponentCalcMethod: CalcMethodFixed, Amount: 100000,
			ComponentMinScore: pgtype.Int4{Int32: 80, Valid: true}},
		{ComponentName: "Insentif", ComponentType: ComponentTypeAllowance, ComponentCalcMethod: CalcMethodFixed, Amount: 500000},
	}

	items, total := dailyAllowanceItems(comps, 22, pgtype.Int4{Int32: 90, Valid: true})
	if len(items) != 2 {
		t.Fatalf("only daily_allowance components count, got %d items", len(items))
	}
	if items[0].Amount != 550000 || items[0].Days != 22 {
		t.Errorf("per present day: got %d over %d days, want 550000 over 22", items[0].Amount, items[0].Days)
	}
	if total != 650000 {
		t.Errorf("total = %d, want 650000", total)
	}

	// Score below the component's min_score withholds it, as payroll does.
	items, total = dailyAllowanceItems(comps, 22, pgtype.Int4{Int32: 70, Valid: true})
	if !items[1].Withheld || items[1].Amount != 0 || total != 550000 {
		t.Errorf("gated: withheld=%v amount=%d total=%d", items[1].Withheld, items[1].Amount, total)
	}

	// No score yet: the gate passes (ScoreGatePasses).
	if _, total = dailyAllowanceItems(comps, 0, pgtype.Int4{}); total != 100000 {
		t.Errorf("no present days, no score: total = %d, want 100000", total)
	}
}
