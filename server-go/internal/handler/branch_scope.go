package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
)

// Branch scoping for the branch_operator role.
//
// middleware.RestrictBranchOperator decides *which endpoints* the role reaches;
// these helpers decide *which rows*. Every handler on that allowlist must do
// one of two things for a scoped caller: force its list onto the caller's
// branch (scopedBranch), or check the employee a request is about
// (requireEmployeeInScope). For every other role both are no-ops.

// scopedBranch returns the branch the caller is confined to. ok is false for
// unscoped callers, in which case the returned UUID is invalid (NULL).
func scopedBranch(r *http.Request) (pgtype.UUID, bool) {
	id, ok := middleware.BranchScopeFromCtx(r.Context())
	if !ok {
		return pgtype.UUID{}, false
	}
	return pgtype.UUID{Bytes: id, Valid: true}, true
}

// scopedBranchString is scopedBranch in the form the hand-built SQL filters
// take (a query-string value cast with ::uuid).
func scopedBranchString(r *http.Request) (string, bool) {
	id, ok := middleware.BranchScopeFromCtx(r.Context())
	if !ok {
		return "", false
	}
	return id.String(), true
}

// requireEmployeeInScope writes a 403 and returns false when a branch-scoped
// caller reaches for an employee outside their branch. An unknown employee
// also fails here for a scoped caller — the handler's own lookup would report
// it as 404, but an operator learns nothing about other branches either way.
func requireEmployeeInScope(w http.ResponseWriter, r *http.Request, queries *db.Queries, employeeID pgtype.UUID) bool {
	branch, ok := scopedBranch(r)
	if !ok {
		return true
	}
	emp, err := queries.GetEmployeeByID(r.Context(), employeeID)
	if err != nil || emp.BranchID != branch {
		respondError(w, http.StatusForbidden, "karyawan ini bukan dari cabang Anda")
		return false
	}
	return true
}
