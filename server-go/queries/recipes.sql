-- name: ListRecipes :many
SELECT
    r.id, r.name, r.batch_size, r.batch_unit_index, r.created_at,
    r.output_item_id, i.name AS output_item_name, i.units AS output_item_units
FROM recipes r
LEFT JOIN items i ON i.id = r.output_item_id
ORDER BY r.name;

-- name: GetRecipeByID :one
SELECT
    r.id, r.name, r.batch_size, r.batch_unit_index, r.created_at,
    r.output_item_id, i.name AS output_item_name, i.units AS output_item_units
FROM recipes r
LEFT JOIN items i ON i.id = r.output_item_id
WHERE r.id = $1;

-- name: GetRecipeIngredients :many
SELECT
    ri.id, ri.recipe_id, ri.item_id, ri.quantity, ri.unit_index,
    i.name AS item_name, i.units AS item_units, i.code AS item_code
FROM recipe_ingredients ri
JOIN items i ON i.id = ri.item_id
WHERE ri.recipe_id = $1
ORDER BY i.name;

-- name: CreateRecipe :one
INSERT INTO recipes (id, name, output_item_id, batch_size, batch_unit_index)
VALUES (gen_random_uuid(), $1, $2, $3, $4)
RETURNING id, name, output_item_id, batch_size, batch_unit_index, created_at;

-- name: CreateRecipeIngredient :exec
INSERT INTO recipe_ingredients (id, recipe_id, item_id, quantity, unit_index)
VALUES (gen_random_uuid(), $1, $2, $3, $4);

-- name: DeleteRecipeIngredients :exec
DELETE FROM recipe_ingredients WHERE recipe_id = $1;

-- name: UpdateRecipe :one
UPDATE recipes SET name = $1, output_item_id = $2, batch_size = $3, batch_unit_index = $4
WHERE id = $5
RETURNING id, name, output_item_id, batch_size, batch_unit_index, created_at;

-- name: DeleteRecipe :exec
DELETE FROM recipes WHERE id = $1;

-- name: GetInventoryQuantityForItem :one
SELECT COALESCE(SUM(quantity), 0)::NUMERIC AS total_quantity
FROM inventory
WHERE item_id = $1 AND warehouse_id = $2;
