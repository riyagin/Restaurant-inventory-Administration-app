package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// ListCategories — GET /api/operational-expense-categories?branch_id=
func (h *OperationalExpensesHandler) ListCategories(w http.ResponseWriter, r *http.Request) {
	sql := `
		SELECT oec.id::text AS id, oec.name, oec.is_system, oec.sort_order,
		       oec.division_id::text AS division_id, d.name AS division_name,
		       d.branch_id::text AS branch_id, b.name AS branch_name,
		       oec.account_id::text AS account_id, a.account_number, a.name AS account_name, a.balance
		FROM operational_expense_categories oec
		JOIN divisions d ON d.id = oec.division_id
		JOIN branches b ON b.id = d.branch_id
		JOIN accounts a ON a.id = oec.account_id`
	var params []any
	if raw := r.URL.Query().Get("branch_id"); raw != "" {
		id, err := parseUUID(raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		sql += ` WHERE d.branch_id = $1`
		params = append(params, pgUUID(id))
	}
	sql += ` ORDER BY b.name, oec.sort_order, oec.name`

	rows, err := h.pool.Query(r.Context(), sql, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil kategori beban operasional")
		return
	}
	categories, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses kategori beban operasional")
		return
	}
	respondJSON(w, http.StatusOK, nilToEmpty(categories))
}

// CreateCategory — POST /api/operational-expense-categories (creates its account too)
func (h *OperationalExpensesHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BranchID string `json:"branch_id"`
		Name     string `json:"name"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.BranchID == "" || body.Name == "" {
		respondError(w, http.StatusBadRequest, "branch_id dan name wajib diisi")
		return
	}
	branchID, err := parseUUID(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "branch_id tidak valid")
		return
	}

	ctx := r.Context()

	divisionID, expenseAccountID, parentName, branchName, err := h.operasionalDivision(ctx, branchID)
	if err != nil {
		respondError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	accountName := parentName + " - " + body.Name

	var accountID pgtype.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO accounts (name, account_number, account_type, parent_id, is_system)
		VALUES ($1,
		        (SELECT COALESCE(MAX(account_number), 49999) + 1 FROM accounts
		         WHERE account_number BETWEEN 50000 AND 59999),
		        'expense', $2, false)
		RETURNING id`, accountName, expenseAccountID).Scan(&accountID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, fmt.Sprintf("akun %q sudah ada", accountName))
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat akun kategori")
		return
	}

	var categoryID pgtype.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO operational_expense_categories (division_id, name, account_id, is_system, sort_order)
		VALUES ($1, $2, $3, false,
		        (SELECT COALESCE(MAX(sort_order), 0) + 10 FROM operational_expense_categories WHERE division_id = $1))
		RETURNING id`, divisionID, body.Name, accountID).Scan(&categoryID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "kategori ini sudah ada di cabang tersebut")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat kategori beban operasional")
		return
	}

	qtx := h.queries.WithTx(tx)
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "operational_expense_category",
		EntityID:    categoryID.Bytes,
		Description: fmt.Sprintf("Menambahkan kategori beban operasional %q di cabang %q beserta akunnya", body.Name, branchName),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kategori")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"id":           uuidBytesToString(categoryID.Bytes),
		"name":         body.Name,
		"division_id":  uuidBytesToString(divisionID.Bytes),
		"branch_id":    body.BranchID,
		"account_id":   uuidBytesToString(accountID.Bytes),
		"account_name": accountName,
		"is_system":    false,
	})
}

// DeleteCategory — DELETE /api/operational-expense-categories/{id}
// Refused for seeded categories and for accounts that have journal lines.
func (h *OperationalExpensesHandler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()

	var name string
	var isSystem bool
	var accountID pgtype.UUID
	var balance int64
	var lines, uses int
	err = h.pool.QueryRow(ctx, `
		SELECT oec.name, oec.is_system, oec.account_id, a.balance,
		       (SELECT COUNT(*) FROM journal_lines jl WHERE jl.account_id = oec.account_id),
		       (SELECT COUNT(*) FROM operational_expenses oe WHERE oe.category_id = oec.id)
		FROM operational_expense_categories oec
		JOIN accounts a ON a.id = oec.account_id
		WHERE oec.id = $1`, pgUUID(id)).Scan(&name, &isSystem, &accountID, &balance, &lines, &uses)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kategori tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kategori")
		return
	}
	if isSystem {
		respondError(w, http.StatusConflict,
			fmt.Sprintf("kategori %q adalah kategori bawaan dan tidak dapat dihapus", name))
		return
	}
	if lines > 0 || uses > 0 || balance != 0 {
		respondError(w, http.StatusConflict,
			fmt.Sprintf("kategori %q sudah dipakai dan tidak dapat dihapus — akunnya menyimpan riwayat transaksi", name))
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM operational_expense_categories WHERE id = $1`, pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus kategori")
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus akun kategori")
		return
	}

	qtx := h.queries.WithTx(tx)
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "operational_expense_category",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus kategori beban operasional %q beserta akunnya", name),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "kategori berhasil dihapus"})
}

func (h *OperationalExpensesHandler) operasionalDivision(ctx context.Context, branchID uuid.UUID) (pgtype.UUID, pgtype.UUID, string, string, error) {
	var divisionID, expenseAccountID pgtype.UUID
	var parentName, branchName string
	err := h.pool.QueryRow(ctx, `
		SELECT d.id, d.expense_account_id, a.name, b.name
		FROM divisions d
		JOIN branches b ON b.id = d.branch_id
		JOIN accounts a ON a.id = d.expense_account_id
		WHERE d.branch_id = $1 AND lower(d.name) = 'operasional'
		ORDER BY d.is_system DESC
		LIMIT 1`, pgUUID(branchID)).Scan(&divisionID, &expenseAccountID, &parentName, &branchName)
	if err != nil {
		return divisionID, expenseAccountID, "", "", errors.New("cabang ini belum punya divisi Operasional beserta akun bebannya")
	}
	return divisionID, expenseAccountID, parentName, branchName, nil
}

// createOperasionalDivision runs inside branch creation, so every branch has one.
func createOperasionalDivision(ctx context.Context, tx pgx.Tx, branchID pgtype.UUID, branchName string) error {
	const nextExpenseNumber = `(SELECT COALESCE(MAX(account_number), 49999) + 1 FROM accounts WHERE account_number BETWEEN 50000 AND 59999)`

	var revID, expID, discID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO accounts (name, account_number, account_type)
		VALUES ($1, (SELECT COALESCE(MAX(account_number), 39999) + 1 FROM accounts
		             WHERE account_number BETWEEN 40000 AND 49999), 'revenue')
		RETURNING id`, "Pendapatan - "+branchName+" - Operasional").Scan(&revID); err != nil {
		return err
	}
	expenseName := "Beban - " + branchName + " - Operasional"
	if err := tx.QueryRow(ctx, `
		INSERT INTO accounts (name, account_number, account_type, is_system)
		VALUES ($1, `+nextExpenseNumber+`, 'expense', true)
		RETURNING id`, expenseName).Scan(&expID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO accounts (name, account_number, account_type)
		VALUES ($1, `+nextExpenseNumber+`, 'expense')
		RETURNING id`, "Diskon - "+branchName+" - Operasional").Scan(&discID); err != nil {
		return err
	}

	var divID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO divisions (branch_id, name, revenue_account_id, expense_account_id, discount_account_id, is_system)
		VALUES ($1, 'Operasional', $2, $3, $4, true)
		RETURNING id`, branchID, revID, expID, discID).Scan(&divID); err != nil {
		return err
	}

	for i, name := range seededOperationalCategories {
		var acctID pgtype.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO accounts (name, account_number, account_type, parent_id, is_system)
			VALUES ($1, `+nextExpenseNumber+`, 'expense', $2, true)
			RETURNING id`, expenseName+" - "+name, expID).Scan(&acctID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO operational_expense_categories (division_id, name, account_id, is_system, sort_order)
			VALUES ($1, $2, $3, true, $4)`, divID, name, acctID, (i+1)*10); err != nil {
			return err
		}
	}
	return nil
}
