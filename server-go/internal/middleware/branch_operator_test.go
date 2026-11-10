package middleware

import (
	"strings"
	"testing"
)

func allowedForBranchOperator(method, path string) bool {
	for _, p := range branchOperatorRoutes {
		if matchRoute(p, method, path) {
			return true
		}
	}
	return false
}

func TestBranchOperatorRoutes(t *testing.T) {
	const id = "0b9f3c1e-6a42-4d0e-9a51-2f7c8d1e4b33"
	allow := []string{
		"GET /api/hr/attendance",
		"PUT /api/hr/attendance/" + id,
		"POST /api/hr/attendance/" + id + "/half-day",
		"GET /api/hr/employees",
		"GET /api/hr/employees/" + id + "/leave-balance",
		"POST /api/hr/leave-requests",
		"POST /api/hr/leave-requests/" + id + "/cancel",
		"GET /api/hr/kasbons/" + id,
		"POST /api/hr/kasbons",
		"POST /api/hr/overtime/" + id + "/cancel",
		"POST /api/auth/logout",
	}
	deny := []string{
		// Approvals, in every module.
		"POST /api/hr/leave-requests/" + id + "/approve",
		"POST /api/hr/leave-requests/bulk-approve",
		"POST /api/hr/kasbons/" + id + "/approve",
		"POST /api/hr/kasbons/" + id + "/process",
		"POST /api/hr/overtime/" + id + "/approve",
		// Right path, wrong method.
		"PUT /api/hr/kasbons/" + id,
		"DELETE /api/hr/overtime/" + id,
		"POST /api/hr/leave-types",
		"PUT /api/hr/employees/" + id + "/leave-balance",
		// Employee detail carries wage and bank data.
		"GET /api/hr/employees/" + id,
		"GET /api/hr/employees/" + id + "/wage",
		// Attendance configuration and batch operations.
		"POST /api/hr/attendance/reconcile",
		"PUT /api/hr/attendance/devices/" + id,
		// Everything outside HR.
		"GET /api/invoices",
		"GET /api/users",
		"POST /api/users",
		"GET /api/reports/financial",
		"GET /api/hr/payroll/periods",
	}
	for _, c := range allow {
		m, p, _ := strings.Cut(c, " ")
		if !allowedForBranchOperator(m, p) {
			t.Errorf("expected %s to be allowed", c)
		}
	}
	for _, c := range deny {
		m, p, _ := strings.Cut(c, " ")
		if allowedForBranchOperator(m, p) {
			t.Errorf("expected %s to be denied", c)
		}
	}
}
