package handler

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type ProductionsHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewProductionsHandler(pool *pgxpool.Pool, queries *db.Queries) *ProductionsHandler {
	return &ProductionsHandler{pool: pool, queries: queries}
}

// List — GET /api/productions
func (h *ProductionsHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var fromDate, toDate pgtype.Date
	if s := r.URL.Query().Get("from"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'from' tidak valid")
			return
		}
		fromDate = pgtype.Date{Time: t, Valid: true}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'to' tidak valid")
			return
		}
		toDate = pgtype.Date{Time: t, Valid: true}
	}

	rows, err := h.queries.ListProductions(ctx, &db.ListProductionsParams{
		Column1: fromDate,
		Column2: toDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data produksi")
		return
	}
	if rows == nil {
		rows = []*db.ListProductionsRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// Create — POST /api/productions
func (h *ProductionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RecipeID    string  `json:"recipe_id"`
		WarehouseID string  `json:"warehouse_id"`
		Batches     float64 `json:"batches"`
		Date        string  `json:"date"`
		Notes       string  `json:"notes"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.RecipeID == "" || body.WarehouseID == "" || body.Batches <= 0 {
		respondError(w, http.StatusBadRequest, "recipe_id, warehouse_id, dan jumlah batch diperlukan")
		return
	}

	recipeID, err := parseUUID(body.RecipeID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "recipe_id tidak valid")
		return
	}
	warehouseID, err := parseUUID(body.WarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
		return
	}

	productionDate := time.Now()
	if body.Date != "" {
		productionDate, err = time.Parse("2006-01-02", body.Date)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal tidak valid")
			return
		}
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	// Load recipe + ingredients (outside transaction — pre-flight only)
	recipe, err := h.queries.GetRecipeByID(ctx, pgtype.UUID{Bytes: recipeID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "resep tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data resep")
		return
	}

	ingredients, err := h.queries.GetRecipeIngredients(ctx, recipe.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil bahan resep")
		return
	}

	// Pre-flight: verify all ingredients have sufficient stock
	for _, ing := range ingredients {
		needed := numericToFloat64(ing.Quantity) * body.Batches
		available, err := h.queries.GetInventoryQuantityForItem(ctx, &db.GetInventoryQuantityForItemParams{
			ItemID:      ing.ItemID,
			WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memeriksa stok bahan")
			return
		}
		avail := numericToFloat64(available)
		if avail < needed {
			respondError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("stok tidak mencukupi untuk bahan: %s, tersedia: %.4f, dibutuhkan: %.4f",
					ing.ItemName, avail, needed))
			return
		}
	}

	// All checks passed — execute in a transaction
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	var totalInputCost int64

	// Deduct each ingredient via FIFO
	for _, ing := range ingredients {
		itemID := ing.ItemID.Bytes
		needed := numericToFloat64(ing.Quantity) * body.Batches

		deductedValue, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, needed)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengurangi stok bahan")
			return
		}
		totalInputCost += deductedValue

		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    warehouseID,
			QuantityChange: -needed,
			UnitName:       ing.ItemName,
			Type:           "production",
			Date:           productionDate,
			Value:          -deductedValue,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok bahan")
			return
		}
	}

	// Add output item to inventory
	outputItemID := recipe.OutputItemID.Bytes
	outputQty := numericToFloat64(recipe.BatchSize) * body.Batches

	if err := service.FIFOAdd(ctx, qtx, outputItemID, warehouseID, outputQty, recipe.BatchUnitIndex, totalInputCost, productionDate); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menambah stok hasil produksi")
		return
	}

	if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
		ItemID:         outputItemID,
		WarehouseID:    warehouseID,
		QuantityChange: outputQty,
		UnitName:       recipe.OutputItemName.String,
		Type:           "production",
		Date:           productionDate,
		Value:          totalInputCost,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok hasil produksi")
		return
	}

	// Insert production record
	prod, err := qtx.InsertProduction(ctx, &db.InsertProductionParams{
		RecipeID:       recipe.ID,
		WarehouseID:    pgtype.UUID{Bytes: warehouseID, Valid: true},
		Batches:        floatToNumeric(body.Batches),
		OutputQuantity: floatToNumeric(outputQty),
		Date:           pgtype.Date{Time: productionDate, Valid: true},
		Notes:          pgtype.Text{String: body.Notes, Valid: body.Notes != ""},
		CreatedBy:      pgtype.UUID{Bytes: userID, Valid: userID != uuid.Nil},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data produksi")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan produksi")
		return
	}

	// Fetch warehouse name for log message
	warehouseRow, _ := h.queries.GetWarehouseByID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true})
	whName := ""
	if warehouseRow != nil {
		whName = warehouseRow.Name
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "Production",
		EntityID:    prod.ID.Bytes,
		Description: fmt.Sprintf("Produksi %.0f batch %s di %s", body.Batches, recipe.Name, whName),
	})

	respondJSON(w, http.StatusCreated, map[string]any{
		"id":              prod.ID,
		"created_at":      prod.CreatedAt,
		"recipe_id":       recipe.ID,
		"recipe_name":     recipe.Name,
		"warehouse_id":    pgtype.UUID{Bytes: warehouseID, Valid: true},
		"warehouse_name":  whName,
		"batches":         body.Batches,
		"output_quantity": outputQty,
		"date":            body.Date,
		"notes":           body.Notes,
	})
}
