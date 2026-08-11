-- name: ListVendorBankAccounts :many
SELECT id, vendor_id, bank_name, account_number, account_holder, bank_branch,
       is_primary, note, created_at
FROM vendor_bank_accounts
WHERE vendor_id = $1
ORDER BY is_primary DESC, bank_name, created_at;

-- Every vendor's accounts in one round trip, so the vendor list can show each
-- one's default destination without a query per row.
-- name: ListAllVendorBankAccounts :many
SELECT id, vendor_id, bank_name, account_number, account_holder, bank_branch,
       is_primary, note, created_at
FROM vendor_bank_accounts
ORDER BY vendor_id, is_primary DESC, bank_name, created_at;

-- name: GetVendorBankAccountByID :one
SELECT id, vendor_id, bank_name, account_number, account_holder, bank_branch,
       is_primary, note, created_at
FROM vendor_bank_accounts WHERE id = $1;

-- name: CreateVendorBankAccount :one
INSERT INTO vendor_bank_accounts (
    id, vendor_id, bank_name, account_number, account_holder, bank_branch, is_primary, note
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
RETURNING id, vendor_id, bank_name, account_number, account_holder, bank_branch,
          is_primary, note, created_at;

-- name: UpdateVendorBankAccount :one
UPDATE vendor_bank_accounts
SET bank_name = $1, account_number = $2, account_holder = $3, bank_branch = $4,
    is_primary = $5, note = $6
WHERE id = $7
RETURNING id, vendor_id, bank_name, account_number, account_holder, bank_branch,
          is_primary, note, created_at;

-- Clear the vendor's current default before naming a new one; the partial unique
-- index allows only one, so this must run first in the same transaction.
-- name: ClearVendorPrimaryBankAccount :exec
UPDATE vendor_bank_accounts SET is_primary = false
WHERE vendor_id = $1 AND is_primary;

-- name: DeleteVendorBankAccount :exec
DELETE FROM vendor_bank_accounts WHERE id = $1;

-- Promote the oldest remaining account after the default was deleted, so a
-- vendor that still has numbers on file never ends up without a default.
-- name: PromoteOldestVendorBankAccount :exec
UPDATE vendor_bank_accounts SET is_primary = true
WHERE id = (
    SELECT id FROM vendor_bank_accounts
    WHERE vendor_id = $1
    ORDER BY created_at
    LIMIT 1
)
AND NOT EXISTS (
    SELECT 1 FROM vendor_bank_accounts WHERE vendor_id = $1 AND is_primary
);
