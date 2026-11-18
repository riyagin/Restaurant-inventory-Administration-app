package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/testutil"
)

// The home division is set on the employee master, must belong to the
// employee's branch, survives an update that does not mention it, and is
// dropped when the employee moves to another branch.
func TestEmployeeHomeDivision(t *testing.T) {
	pool := testutil.OpenDB(t)
	bg := context.Background()
	userID := createTestUser(t, pool)
	ctx := middleware.ContextWithClaims(bg, testClaims(userID))
	h := handler.NewHREmployeesHandler(pool, db.New(pool))

	suffix := uuid.NewString()[:8]
	branchA, branchB := uuid.New(), uuid.New()
	divA, divB := uuid.New(), uuid.New()
	positionID := uuid.New()
	for _, b := range []struct {
		id   uuid.UUID
		name string
	}{{branchA, "BrDivA-" + suffix}, {branchB, "BrDivB-" + suffix}} {
		if _, err := pool.Exec(bg, `INSERT INTO branches (id, name) VALUES ($1, $2)`, b.id, b.name); err != nil {
			t.Fatalf("branch: %v", err)
		}
	}
	if _, err := pool.Exec(bg, `INSERT INTO divisions (id, branch_id, name) VALUES ($1, $2, 'DivA-'||$4), ($3, $5, 'DivB-'||$4)`,
		divA, branchA, divB, suffix, branchB); err != nil {
		t.Fatalf("divisions: %v", err)
	}
	if _, err := pool.Exec(bg, `INSERT INTO positions (id, name, is_active) VALUES ($1, 'PosDiv-'||$2, true)`, positionID, suffix); err != nil {
		t.Fatalf("position: %v", err)
	}
	var empID string
	t.Cleanup(func() {
		if empID != "" {
			pool.Exec(bg, `DELETE FROM employees WHERE id = $1`, empID)
		}
		pool.Exec(bg, `DELETE FROM positions WHERE id = $1`, positionID)
		pool.Exec(bg, `DELETE FROM divisions WHERE id IN ($1, $2)`, divA, divB)
		pool.Exec(bg, `DELETE FROM branches WHERE id IN ($1, $2)`, branchA, branchB)
	})

	body := func(branch uuid.UUID, division *string) map[string]any {
		m := map[string]any{
			"employee_code": "EMPDIV-" + suffix,
			"full_name":     "Uji Divisi " + suffix,
			"join_date":     "2026-01-05",
			"position_id":   positionID.String(),
			"branch_id":     branch.String(),
		}
		if division != nil {
			m["division_id"] = *division
		}
		return m
	}
	str := func(u uuid.UUID) *string { s := u.String(); return &s }
	homeDivision := func() *string {
		var d *string
		if err := pool.QueryRow(bg, `SELECT division_id::text FROM employees WHERE id = $1`, empID).Scan(&d); err != nil {
			t.Fatalf("read division: %v", err)
		}
		return d
	}

	// Another branch's division is refused.
	if rr := postJSON(t, h.Create, ctx, body(branchA, str(divB))); rr.Code != http.StatusBadRequest {
		t.Fatalf("create with foreign division: want 400, got %d %s", rr.Code, rr.Body)
	}

	rr := postJSON(t, h.Create, ctx, body(branchA, str(divA)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d %s", rr.Code, rr.Body)
	}
	var created struct {
		ID           string  `json:"id"`
		DivisionID   *string `json:"division_id"`
		DivisionName *string `json:"division_name"`
	}
	decodeJSON(t, rr, &created)
	empID = created.ID
	if created.DivisionID == nil || *created.DivisionID != divA.String() || created.DivisionName == nil {
		t.Fatalf("create response division = %v / %v, want %s", created.DivisionID, created.DivisionName, divA)
	}

	// An update that omits division_id (onboarding, older callers) keeps it.
	if rr := callWithID(t, h.Update, ctx, http.MethodPut, empID, body(branchA, nil)); rr.Code != http.StatusOK {
		t.Fatalf("update without division: %d %s", rr.Code, rr.Body)
	}
	if d := homeDivision(); d == nil || *d != divA.String() {
		t.Fatalf("division after omitted update = %v, want %s", d, divA)
	}

	// Moving branch without naming a division drops the old branch's division.
	if rr := callWithID(t, h.Update, ctx, http.MethodPut, empID, body(branchB, nil)); rr.Code != http.StatusOK {
		t.Fatalf("update branch: %d %s", rr.Code, rr.Body)
	}
	if d := homeDivision(); d != nil {
		t.Fatalf("division after branch move = %v, want NULL", *d)
	}

	// The new branch's division can be set; the old one cannot.
	if rr := callWithID(t, h.Update, ctx, http.MethodPut, empID, body(branchB, str(divA))); rr.Code != http.StatusBadRequest {
		t.Fatalf("update with foreign division: want 400, got %d %s", rr.Code, rr.Body)
	}
	if rr := callWithID(t, h.Update, ctx, http.MethodPut, empID, body(branchB, str(divB))); rr.Code != http.StatusOK {
		t.Fatalf("update with own division: %d %s", rr.Code, rr.Body)
	}
	if d := homeDivision(); d == nil || *d != divB.String() {
		t.Fatalf("division = %v, want %s", d, divB)
	}

	// An explicit empty value clears it.
	empty := ""
	if rr := callWithID(t, h.Update, ctx, http.MethodPut, empID, body(branchB, &empty)); rr.Code != http.StatusOK {
		t.Fatalf("clear division: %d %s", rr.Code, rr.Body)
	}
	if d := homeDivision(); d != nil {
		t.Fatalf("division after clear = %v, want NULL", *d)
	}
}
