package service

import "testing"

// The payroll entry must balance to the rupiah: debits (per-branch wage expense)
// against credits (cash paid out + kasbon receivable cleared). An unbalanced entry
// is rejected by Post and by a DB trigger, so this pins the arithmetic that feeds
// them rather than waiting for a runtime failure at month end.
func TestPayrollEntryBalances(t *testing.T) {
	branches := []struct {
		net    int64
		kasbon int64
	}{
		{net: 12_000_000, kasbon: 500_000},
		{net: 7_500_000, kasbon: 0},
		{net: 3_250_000, kasbon: 250_000},
	}

	var debits, totalNet, totalKasbon int64
	for _, b := range branches {
		debits += branchExpenseAmount(b.net, b.kasbon)
		totalNet += b.net
		totalKasbon += b.kasbon
	}

	credits := totalNet + totalKasbon
	if debits != credits {
		t.Fatalf("entry unbalanced: debits=%d credits=%d", debits, credits)
	}
	if debits != 23_500_000 {
		t.Errorf("wage expense = %d, want 23500000 (net + kasbon)", debits)
	}
}

// A branch whose employees are all fully deducted (net and kasbon both zero)
// contributes no leg — a zero leg carries no information and Post drops it anyway.
func TestBranchExpenseAmountZero(t *testing.T) {
	if got := branchExpenseAmount(0, 0); got != 0 {
		t.Errorf("branchExpenseAmount(0, 0) = %d, want 0", got)
	}
}

func TestWageAccountName(t *testing.T) {
	if got := WageAccountName("Cabang Pusat"); got != "Beban Gaji - Cabang Pusat" {
		t.Errorf("WageAccountName = %q, want %q", got, "Beban Gaji - Cabang Pusat")
	}
}
