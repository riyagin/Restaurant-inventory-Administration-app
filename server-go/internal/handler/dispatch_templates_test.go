package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/testutil"
)

// dispatchTemplateFixtures holds the master data a template can point at.
type dispatchTemplateFixtures struct {
	itemA       uuid.UUID
	itemB       uuid.UUID
	warehouseID uuid.UUID
	branchID    uuid.UUID
	divisionID  uuid.UUID
}

// setupDispatchTemplateFixtures inserts two items, a warehouse, and a branch
// with one division. No inventory: a template never touches stock.
func setupDispatchTemplateFixtures(t *testing.T, pool *pgxpool.Pool) dispatchTemplateFixtures {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.New().String()[:8]

	itemA, itemB := uuid.New(), uuid.New()
	warehouseID, branchID, divisionID := uuid.New(), uuid.New(), uuid.New()

	for i, id := range []uuid.UUID{itemA, itemB} {
		tag := fmt.Sprintf("%s%d", suffix, i)
		if _, err := pool.Exec(ctx,
			`INSERT INTO items (id, name, code, units, is_stock)
			 VALUES ($1, 'TplItem-'||$2, 'TPI-'||$2,
			         '[{"name":"dus","perPrev":null},{"name":"pcs","perPrev":10}]', true)`,
			id, tag); err != nil {
			t.Fatalf("insert item %d: %v", i, err)
		}
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (id, name) VALUES ($1, 'WHTpl-'||$2)`, warehouseID, suffix); err != nil {
		t.Fatalf("insert warehouse: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO branches (id, name) VALUES ($1, 'BrTpl-'||$2)`, branchID, suffix); err != nil {
		t.Fatalf("insert branch: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO divisions (id, branch_id, name) VALUES ($1, $2, 'DivTpl-'||$3)`,
		divisionID, branchID, suffix); err != nil {
		t.Fatalf("insert division: %v", err)
	}

	t.Cleanup(func() {
		bCtx := context.Background()
		pool.Exec(bCtx, `DELETE FROM dispatch_template_items WHERE item_id IN ($1, $2)`, itemA, itemB)
		pool.Exec(bCtx, `DELETE FROM dispatch_templates WHERE warehouse_id = $1 OR branch_id = $2`, warehouseID, branchID)
		pool.Exec(bCtx, `DELETE FROM divisions WHERE id = $1`, divisionID)
		pool.Exec(bCtx, `DELETE FROM branches WHERE id = $1`, branchID)
		pool.Exec(bCtx, `DELETE FROM warehouses WHERE id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM items WHERE id IN ($1, $2)`, itemA, itemB)
	})

	return dispatchTemplateFixtures{
		itemA: itemA, itemB: itemB,
		warehouseID: warehouseID, branchID: branchID, divisionID: divisionID,
	}
}

// TestDispatchTemplateCRUD walks the whole lifecycle: create with two items,
// read it back, replace the item list via update, then delete.
func TestDispatchTemplateCRUD(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupDispatchTemplateFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewDispatchTemplatesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	name := "Tpl-" + uuid.New().String()[:8]

	// ── Create ────────────────────────────────────────────────────────────────
	rr := postJSON(t, h.Create, ctx, map[string]any{
		"name":         name,
		"warehouse_id": fix.warehouseID.String(),
		"branch_id":    fix.branchID.String(),
		"division_id":  fix.divisionID.String(),
		"notes":        "kirim pagi",
		"items": []map[string]any{
			{"item_id": fix.itemA.String(), "unit_index": 1},
			{"item_id": fix.itemB.String(), "unit_index": 0},
		},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, rr, &created)
	if created.ID == "" {
		t.Fatal("create response has no id")
	}

	// ── Get ───────────────────────────────────────────────────────────────────
	rr = callWithID(t, h.Get, ctx, http.MethodGet, created.ID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var fetched struct {
		Name          string  `json:"name"`
		Notes         *string `json:"notes"`
		WarehouseName *string `json:"warehouse_name"`
		DivisionName  *string `json:"division_name"`
		Items         []struct {
			ItemID    string `json:"item_id"`
			UnitIndex int32  `json:"unit_index"`
			SortOrder int32  `json:"sort_order"`
		} `json:"items"`
	}
	decodeJSON(t, rr, &fetched)
	if fetched.Name != name {
		t.Errorf("name = %q, want %q", fetched.Name, name)
	}
	if fetched.Notes == nil || *fetched.Notes != "kirim pagi" {
		t.Errorf("notes = %v, want \"kirim pagi\"", fetched.Notes)
	}
	if fetched.WarehouseName == nil || fetched.DivisionName == nil {
		t.Error("warehouse_name/division_name should be joined into the response")
	}
	if len(fetched.Items) != 2 {
		t.Fatalf("item count = %d, want 2", len(fetched.Items))
	}
	// Items come back in sort_order, which is payload order.
	if fetched.Items[0].ItemID != fix.itemA.String() || fetched.Items[0].UnitIndex != 1 {
		t.Errorf("first item = %+v, want itemA at unit 1", fetched.Items[0])
	}
	if fetched.Items[1].SortOrder != 1 {
		t.Errorf("second item sort_order = %d, want 1", fetched.Items[1].SortOrder)
	}

	// ── Update: replace the item list with a single row ───────────────────────
	rr = callWithID(t, h.Update, ctx, http.MethodPut, created.ID, map[string]any{
		"name":         name + "-rev",
		"warehouse_id": fix.warehouseID.String(),
		"branch_id":    fix.branchID.String(),
		"division_id":  nil,
		"items": []map[string]any{
			{"item_id": fix.itemB.String(), "unit_index": 1},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var itemCount int
	pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM dispatch_template_items WHERE template_id = $1`, created.ID).Scan(&itemCount)
	if itemCount != 1 {
		t.Errorf("after update, item count = %d, want 1 (old rows must be replaced)", itemCount)
	}
	var divisionNull bool
	pool.QueryRow(context.Background(),
		`SELECT division_id IS NULL FROM dispatch_templates WHERE id = $1`, created.ID).Scan(&divisionNull)
	if !divisionNull {
		t.Error("clearing division_id on update should store NULL")
	}

	// ── Delete ────────────────────────────────────────────────────────────────
	rr = callWithID(t, h.Delete, ctx, http.MethodDelete, created.ID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var remaining int
	pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM dispatch_templates WHERE id = $1`, created.ID).Scan(&remaining)
	if remaining != 0 {
		t.Errorf("template still present after delete")
	}
	// The items must go with it (ON DELETE CASCADE).
	pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM dispatch_template_items WHERE template_id = $1`, created.ID).Scan(&itemCount)
	if itemCount != 0 {
		t.Errorf("orphan template items = %d, want 0", itemCount)
	}
}

// TestDispatchTemplateCreate_NameRequired rejects a blank name — the name is
// the button label on the dispatch form, so an empty one is unusable.
func TestDispatchTemplateCreate_NameRequired(t *testing.T) {
	pool := testutil.OpenDB(t)
	userID := createTestUser(t, pool)

	h := handler.NewDispatchTemplatesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Create, ctx, map[string]any{"name": "   ", "items": []any{}})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestDispatchTemplateCreate_HeaderOptional confirms a template needs nothing
// but a name: destination and items can be filled in on the dispatch form.
func TestDispatchTemplateCreate_HeaderOptional(t *testing.T) {
	pool := testutil.OpenDB(t)
	userID := createTestUser(t, pool)

	h := handler.NewDispatchTemplatesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	name := "TplBare-" + uuid.New().String()[:8]
	rr := postJSON(t, h.Create, ctx, map[string]any{"name": name})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, rr, &created)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM dispatch_templates WHERE id = $1`, created.ID)
	})

	rr = callWithID(t, h.Get, ctx, http.MethodGet, created.ID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var fetched struct {
		WarehouseID *string `json:"warehouse_id"`
		Items       []any   `json:"items"`
	}
	decodeJSON(t, rr, &fetched)
	if fetched.WarehouseID != nil {
		t.Errorf("warehouse_id = %v, want null", *fetched.WarehouseID)
	}
	if len(fetched.Items) != 0 {
		t.Errorf("items = %d, want 0", len(fetched.Items))
	}
}
