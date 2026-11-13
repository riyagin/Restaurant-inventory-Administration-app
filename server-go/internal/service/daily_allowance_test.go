package service

import (
	"testing"

	"inventory-app/server-go/internal/db"
)

func TestDailyAllowanceItems(t *testing.T) {
	comps := []*db.ListEmployeeWageComponentsRow{
		{ComponentName: "Uang Makan", ComponentType: ComponentTypeDailyAllowance, ComponentCalcMethod: CalcMethodPerPresentDay, Amount: 25000},
		{ComponentName: "Uang Transport", ComponentType: ComponentTypeDailyAllowance, ComponentCalcMethod: CalcMethodFixed, Amount: 100000,
			ComponentGradeLinked: true},
		{ComponentName: "Insentif", ComponentType: ComponentTypeAllowance, ComponentCalcMethod: CalcMethodFixed, Amount: 500000},
	}

	items, total := dailyAllowanceItems(comps, 22, &GradeTier{Label: "A", MinScore: 90, PayPercent: 100})
	if len(items) != 2 {
		t.Fatalf("only daily_allowance components count, got %d items", len(items))
	}
	if items[0].Amount != 550000 || items[0].Days != 22 {
		t.Errorf("per present day: got %d over %d days, want 550000 over 22", items[0].Amount, items[0].Days)
	}
	if total != 650000 {
		t.Errorf("total = %d, want 650000", total)
	}

	// A lower grade pays only its share of the grade-linked component.
	items, total = dailyAllowanceItems(comps, 22, &GradeTier{Label: "C", MinScore: 70, PayPercent: 50})
	if items[1].Amount != 50000 || items[1].FullAmount != 100000 || items[1].Grade != "C" || total != 600000 {
		t.Errorf("graded: amount=%d full=%d grade=%q total=%d", items[1].Amount, items[1].FullAmount, items[1].Grade, total)
	}
	if items[0].Amount != 550000 || items[0].Grade != "" {
		t.Errorf("unlinked component must ignore the grade: amount=%d grade=%q", items[0].Amount, items[0].Grade)
	}

	// No score yet (no tier): paid in full.
	if _, total = dailyAllowanceItems(comps, 0, nil); total != 100000 {
		t.Errorf("no present days, no score: total = %d, want 100000", total)
	}
}
