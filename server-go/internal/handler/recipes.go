package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type RecipesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewRecipesHandler(pool *pgxpool.Pool, queries *db.Queries) *RecipesHandler {
	return &RecipesHandler{pool: pool, queries: queries}
}

// List — GET /api/recipes
func (h *RecipesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	recipes, err := h.queries.ListRecipes(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data resep")
		return
	}

	type recipeWithIngredients struct {
		*db.ListRecipesRow
		Ingredients []*db.GetRecipeIngredientsRow `json:"ingredients"`
	}

	result := make([]recipeWithIngredients, 0, len(recipes))
	for _, rec := range recipes {
		ings, err := h.queries.GetRecipeIngredients(ctx, rec.ID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil bahan resep")
			return
		}
		if ings == nil {
			ings = []*db.GetRecipeIngredientsRow{}
		}
		result = append(result, recipeWithIngredients{ListRecipesRow: rec, Ingredients: ings})
	}

	respondJSON(w, http.StatusOK, result)
}

// Get — GET /api/recipes/:id
func (h *RecipesHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	recipe, err := h.queries.GetRecipeByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "resep tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data resep")
		return
	}

	ings, err := h.queries.GetRecipeIngredients(ctx, recipe.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil bahan resep")
		return
	}
	if ings == nil {
		ings = []*db.GetRecipeIngredientsRow{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"recipe":      recipe,
		"ingredients": ings,
	})
}

// Create — POST /api/recipes (admin)
func (h *RecipesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string  `json:"name"`
		OutputItemID   string  `json:"output_item_id"`
		BatchSize      float64 `json:"batch_size"`
		BatchUnitIndex int32   `json:"batch_unit_index"`
		Ingredients    []struct {
			ItemID    string  `json:"item_id"`
			Quantity  float64 `json:"quantity"`
			UnitIndex int32   `json:"unit_index"`
		} `json:"ingredients"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.Name == "" || body.OutputItemID == "" || body.BatchSize <= 0 || len(body.Ingredients) == 0 {
		respondError(w, http.StatusBadRequest, "nama, item output, ukuran batch, dan minimal satu bahan diperlukan")
		return
	}

	outputItemID, err := parseUUID(body.OutputItemID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "output_item_id tidak valid")
		return
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	var batchSizeNumeric pgtype.Numeric
	_ = batchSizeNumeric.Scan(body.BatchSize)

	recipe, err := qtx.CreateRecipe(ctx, &db.CreateRecipeParams{
		Name:           body.Name,
		OutputItemID:   pgtype.UUID{Bytes: outputItemID, Valid: true},
		BatchSize:      batchSizeNumeric,
		BatchUnitIndex: body.BatchUnitIndex,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat resep")
		return
	}

	for _, ing := range body.Ingredients {
		itemID, err := parseUUID(ing.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", ing.ItemID))
			return
		}
		var qtyNumeric pgtype.Numeric
		_ = qtyNumeric.Scan(ing.Quantity)
		if err := qtx.CreateRecipeIngredient(ctx, &db.CreateRecipeIngredientParams{
			RecipeID:  recipe.ID,
			ItemID:    pgtype.UUID{Bytes: itemID, Valid: true},
			Quantity:  qtyNumeric,
			UnitIndex: ing.UnitIndex,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan bahan resep")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan resep")
		return
	}

	ings, _ := h.queries.GetRecipeIngredients(ctx, recipe.ID)
	if ings == nil {
		ings = []*db.GetRecipeIngredientsRow{}
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "Recipe",
		EntityID:    recipe.ID.Bytes,
		Description: fmt.Sprintf("Buat resep \"%s\"", recipe.Name),
	})

	respondJSON(w, http.StatusCreated, map[string]any{
		"recipe":      recipe,
		"ingredients": ings,
	})
}

// Update — PUT /api/recipes/:id (admin)
func (h *RecipesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Name           string  `json:"name"`
		OutputItemID   string  `json:"output_item_id"`
		BatchSize      float64 `json:"batch_size"`
		BatchUnitIndex int32   `json:"batch_unit_index"`
		Ingredients    []struct {
			ItemID    string  `json:"item_id"`
			Quantity  float64 `json:"quantity"`
			UnitIndex int32   `json:"unit_index"`
		} `json:"ingredients"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.Name == "" || body.OutputItemID == "" || body.BatchSize <= 0 || len(body.Ingredients) == 0 {
		respondError(w, http.StatusBadRequest, "nama, item output, ukuran batch, dan minimal satu bahan diperlukan")
		return
	}

	outputItemID, err := parseUUID(body.OutputItemID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "output_item_id tidak valid")
		return
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	var batchSizeNumeric pgtype.Numeric
	_ = batchSizeNumeric.Scan(body.BatchSize)

	recipe, err := qtx.UpdateRecipe(ctx, &db.UpdateRecipeParams{
		Name:           body.Name,
		OutputItemID:   pgtype.UUID{Bytes: outputItemID, Valid: true},
		BatchSize:      batchSizeNumeric,
		BatchUnitIndex: body.BatchUnitIndex,
		ID:             pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "resep tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui resep")
		return
	}

	if err := qtx.DeleteRecipeIngredients(ctx, recipe.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus bahan lama")
		return
	}

	for _, ing := range body.Ingredients {
		itemID, err := parseUUID(ing.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", ing.ItemID))
			return
		}
		var qtyNumeric pgtype.Numeric
		_ = qtyNumeric.Scan(ing.Quantity)
		if err := qtx.CreateRecipeIngredient(ctx, &db.CreateRecipeIngredientParams{
			RecipeID:  recipe.ID,
			ItemID:    pgtype.UUID{Bytes: itemID, Valid: true},
			Quantity:  qtyNumeric,
			UnitIndex: ing.UnitIndex,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan bahan resep")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan resep")
		return
	}

	ings, _ := h.queries.GetRecipeIngredients(ctx, recipe.ID)
	if ings == nil {
		ings = []*db.GetRecipeIngredientsRow{}
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "UPDATE",
		EntityType:  "Recipe",
		EntityID:    id,
		Description: fmt.Sprintf("Perbarui resep \"%s\"", recipe.Name),
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"recipe":      recipe,
		"ingredients": ings,
	})
}

// Delete — DELETE /api/recipes/:id (admin)
func (h *RecipesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	recipe, err := h.queries.GetRecipeByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "resep tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data resep")
		return
	}

	if err := h.queries.DeleteRecipe(ctx, pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus resep")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "DELETE",
		EntityType:  "Recipe",
		EntityID:    id,
		Description: fmt.Sprintf("Hapus resep \"%s\"", recipe.Name),
	})

	w.WriteHeader(http.StatusNoContent)
}
