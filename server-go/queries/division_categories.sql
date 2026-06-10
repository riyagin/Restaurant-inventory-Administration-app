-- name: ListDivisionCategories :many
SELECT id, division_id, name FROM division_categories
WHERE ($1::uuid IS NULL OR division_id = $1)
ORDER BY name;

-- name: CreateDivisionCategory :one
INSERT INTO division_categories (id, division_id, name)
VALUES (gen_random_uuid(), $1, $2)
RETURNING id, division_id, name;

-- name: DeleteDivisionCategory :exec
DELETE FROM division_categories WHERE id = $1;
