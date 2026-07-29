package service

import "testing"

func TestNormalizeLogAction(t *testing.T) {
	cases := map[string]string{
		"CREATE": "create",
		"UPDATE": "update",
		"DELETE": "delete",
		"create": "create",
		" 	DELETE ": "delete",
		"":       "",
	}
	for in, want := range cases {
		if got := NormalizeLogAction(in); got != want {
			t.Errorf("NormalizeLogAction(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeLogEntityType(t *testing.T) {
	cases := map[string]string{
		// PascalCase spellings used by the pre-existing Go handlers.
		"Invoice":           "invoice",
		"Inventory":         "inventory",
		"StockTransfer":     "stock_transfer",
		"StockOpname":       "stock_opname",
		"AccountAdjustment": "account_adjustment",
		"CapitalInjection":  "capital_injection",
		// Acronym runs must not split per letter.
		"POSImport": "pos_import",
		// Already-normalized values pass through untouched.
		"hr_employee":    "hr_employee",
		"payroll_period": "payroll_period",
		"item":           "item",
		"": "",
	}
	for in, want := range cases {
		if got := NormalizeLogEntityType(in); got != want {
			t.Errorf("NormalizeLogEntityType(%q) = %q, want %q", in, got, want)
		}
	}
}

// Normalization must be idempotent: re-normalizing an already-folded value is a
// no-op, so the migration backfill and the runtime path cannot disagree.
func TestNormalizeLogEntityTypeIdempotent(t *testing.T) {
	for _, in := range []string{"Invoice", "StockTransfer", "POSImport", "hr_employee"} {
		once := NormalizeLogEntityType(in)
		if twice := NormalizeLogEntityType(once); twice != once {
			t.Errorf("not idempotent for %q: %q → %q", in, once, twice)
		}
	}
}
