package handler

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type POSImportHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewPOSImportHandler(pool *pgxpool.Pool, queries *db.Queries) *POSImportHandler {
	return &POSImportHandler{pool: pool, queries: queries}
}

// Parse — POST /api/pos-import/parse
// Accepts a multipart Excel upload and returns parsed data as JSON preview (no DB writes).
func (h *POSImportHandler) Parse(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "gagal membaca form (maks 20 MB)")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "tidak ada file yang diunggah")
		return
	}
	defer file.Close()

	result, err := service.ParsePOSExcel(file, header.Filename)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// confirmMapping is one line in a confirm request (revenue / cash / discount / expense).
type confirmMapping struct {
	Label     string `json:"label"`
	AccountID string `json:"account_id"`
	Amount    int64  `json:"amount"`
}

// confirmEntry is one date-group in the confirm request body.
type confirmEntry struct {
	Date             string           `json:"date"`
	Description      string           `json:"description"`
	RevenueMappings  []confirmMapping `json:"revenue_mappings"`
	CashMappings     []confirmMapping `json:"cash_mappings"`
	DiscountMappings []confirmMapping `json:"discount_mappings"`
	ExpenseMappings  []confirmMapping `json:"expense_mappings"`
}

// Confirm — POST /api/pos-import/confirm
// Saves parsed data with account mappings, updating account balances per entry.
func (h *POSImportHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Imports  []confirmEntry `json:"imports"`
		Filename string         `json:"filename"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if len(body.Imports) == 0 {
		respondError(w, http.StatusBadRequest, "data tidak lengkap")
		return
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	// Validate before touching the DB.
	for _, entry := range body.Imports {
		if entry.Date == "" {
			respondError(w, http.StatusBadRequest, "tanggal wajib diisi")
			return
		}
		for _, m := range append(entry.RevenueMappings, entry.CashMappings...) {
			if m.AccountID == "" {
				respondError(w, http.StatusBadRequest, fmt.Sprintf(`akun belum dipilih untuk "%s"`, m.Label))
				return
			}
			if m.Amount <= 0 {
				respondError(w, http.StatusBadRequest, fmt.Sprintf(`jumlah tidak valid untuk "%s"`, m.Label))
				return
			}
		}
		for _, m := range entry.DiscountMappings {
			if m.AccountID == "" {
				respondError(w, http.StatusBadRequest, "akun diskon belum dipilih")
				return
			}
		}
		for _, m := range entry.ExpenseMappings {
			if m.AccountID == "" {
				respondError(w, http.StatusBadRequest, "akun biaya tambahan belum dipilih")
				return
			}
		}
	}

	type savedImport struct {
		ID          pgtype.UUID        `json:"id"`
		Description pgtype.Text        `json:"description"`
		Date        pgtype.Date        `json:"date"`
		SourceFile  pgtype.Text        `json:"source_file"`
		TotalAmount int64              `json:"total_amount"`
		CreatedAt   pgtype.Timestamptz `json:"created_at"`
	}

	var savedImports []savedImport

	for _, entry := range body.Imports {
		saleDate, err := time.Parse("2006-01-02", entry.Date)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("format tanggal tidak valid: %s", entry.Date))
			return
		}

		totalRevenue := int64(0)
		for _, m := range entry.RevenueMappings {
			totalRevenue += m.Amount
		}

		description := entry.Description
		if description == "" {
			description = fmt.Sprintf("POS Import %s", entry.Date)
		}

		tx, err := h.pool.Begin(ctx)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
			return
		}

		qtx := h.queries.WithTx(tx)

		// One journal entry per import: Dr cash, Dr expense, Cr revenue.
		//
		// Discount lines are still not posted. The revenue figures the POS
		// exports are already net of discount (across the existing imports,
		// recorded cash tracks recorded revenue to within a rounding error while
		// discounts total ~51M), so posting them again would double-count.
		//
		// The expense lines are the ones that used to have no counter-leg at all,
		// which is where 7,128,062 of the drift came from. A till expense has to
		// be balanced by something; where the import's own numbers do not add up,
		// the residual goes to the suspense account with a memo rather than being
		// silently dropped.
		var journalLines []service.Line
		var net int64

		addLine := func(m confirmMapping, debit bool) bool {
			accountID, err := parseUUID(m.AccountID)
			if err != nil {
				tx.Rollback(ctx)
				respondError(w, http.StatusBadRequest, fmt.Sprintf("account_id tidak valid untuk \"%s\"", m.Label))
				return false
			}
			line := service.Cr(accountID, m.Amount)
			if debit {
				line = service.Dr(accountID, m.Amount)
			}
			journalLines = append(journalLines, line.WithMemo(m.Label))
			net += line.Amount
			return true
		}

		for _, m := range entry.CashMappings {
			if !addLine(m, true) {
				return
			}
		}
		for _, m := range entry.ExpenseMappings {
			if !addLine(m, true) {
				return
			}
		}
		for _, m := range entry.RevenueMappings {
			if !addLine(m, false) {
				return
			}
		}

		if net != 0 {
			suspense, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: service.SuspenseAccountNumber, Valid: true})
			if err != nil {
				tx.Rollback(ctx)
				respondError(w, http.StatusInternalServerError, "akun sementara tidak ditemukan")
				return
			}
			journalLines = append(journalLines,
				service.Dr(suspense.ID.Bytes, -net).WithMemo("selisih impor POS (kas + beban ≠ pendapatan)"))
		}

		imp, err := qtx.InsertPOSImport(ctx, &db.InsertPOSImportParams{
			Description: pgtype.Text{String: description, Valid: true},
			Date:        pgtype.Date{Time: saleDate, Valid: true},
			SourceFile:  pgtype.Text{String: body.Filename, Valid: body.Filename != ""},
			TotalAmount: totalRevenue,
			CreatedBy:   pgtype.UUID{Bytes: userID, Valid: userID.String() != "00000000-0000-0000-0000-000000000000"},
		})
		if err != nil {
			tx.Rollback(ctx)
			respondError(w, http.StatusInternalServerError, "gagal menyimpan import")
			return
		}

		// Posted after the import row exists so the entry carries its source id.
		if _, err := service.Post(ctx, qtx, service.Entry{
			Date:        saleDate,
			SourceType:  service.SourcePOSImport,
			SourceID:    imp.ID.Bytes,
			Description: description,
			CreatedBy:   userID,
			Lines:       journalLines,
		}); err != nil {
			tx.Rollback(ctx)
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal impor POS")
			return
		}

		// Insert lines for all four types.
		type lineInsert struct {
			mappings []confirmMapping
			lineType string
		}
		for _, group := range []lineInsert{
			{entry.RevenueMappings, "revenue"},
			{entry.DiscountMappings, "discount"},
			{entry.ExpenseMappings, "expense"},
			{entry.CashMappings, "cash"},
		} {
			for _, m := range group.mappings {
				accountID, err := parseUUID(m.AccountID)
				if err != nil {
					tx.Rollback(ctx)
					respondError(w, http.StatusBadRequest, "account_id tidak valid")
					return
				}
				if err := qtx.InsertPOSImportLine(ctx, &db.InsertPOSImportLineParams{
					ImportID:  imp.ID,
					AccountID: pgtype.UUID{Bytes: accountID, Valid: true},
					Label:     m.Label,
					Amount:    m.Amount,
					LineType:  group.lineType,
				}); err != nil {
					tx.Rollback(ctx)
					respondError(w, http.StatusInternalServerError, "gagal menyimpan baris import")
					return
				}
			}
		}

		if err := tx.Commit(ctx); err != nil {
			tx.Rollback(ctx)
			respondError(w, http.StatusInternalServerError, "gagal menyimpan import")
			return
		}

		_ = service.LogActivity(ctx, h.queries, service.LogParams{
			UserID:      userID,
			Username:    username,
			Action:      "CREATE",
			EntityType:  "POSImport",
			EntityID:    imp.ID.Bytes,
			Description: fmt.Sprintf("POS Import %s: %d (%d kategori, %d metode bayar)", entry.Date, totalRevenue, len(entry.RevenueMappings), len(entry.CashMappings)),
		})

		savedImports = append(savedImports, savedImport{
			ID:          imp.ID,
			Description: pgtype.Text{String: description, Valid: true},
			Date:        pgtype.Date{Time: saleDate, Valid: true},
			SourceFile:  pgtype.Text{String: body.Filename, Valid: body.Filename != ""},
			TotalAmount: totalRevenue,
			CreatedAt:   imp.CreatedAt,
		})
	}

	if len(savedImports) == 1 {
		respondJSON(w, http.StatusCreated, savedImports[0])
	} else {
		respondJSON(w, http.StatusCreated, savedImports)
	}
}

// List — GET /api/pos-import
// Returns all imports with embedded lines, matching Node.js response shape.
func (h *POSImportHandler) List(w http.ResponseWriter, r *http.Request) {
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

	imports, err := h.queries.ListPOSImports(ctx, &db.ListPOSImportsParams{
		Column1: fromDate,
		Column2: toDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data import")
		return
	}
	if imports == nil {
		imports = []*db.ListPOSImportsRow{}
	}

	lines, err := h.queries.ListAllPOSImportLines(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris import")
		return
	}

	// Group lines by import_id.
	linesByImport := map[pgtype.UUID][]*db.ListAllPOSImportLinesRow{}
	for _, l := range lines {
		linesByImport[l.ImportID] = append(linesByImport[l.ImportID], l)
	}

	type importWithLines struct {
		*db.ListPOSImportsRow
		Lines []*db.ListAllPOSImportLinesRow `json:"lines"`
	}

	result := make([]importWithLines, len(imports))
	for i, imp := range imports {
		lns := linesByImport[imp.ID]
		if lns == nil {
			lns = []*db.ListAllPOSImportLinesRow{}
		}
		result[i] = importWithLines{imp, lns}
	}

	respondJSON(w, http.StatusOK, result)
}

// Delete — DELETE /api/pos-import/:id (admin only)
// Reverses all non-discount account balance changes, then deletes the import.
func (h *POSImportHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	imp, err := h.queries.GetPOSImportByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "import tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data import")
		return
	}

	lines, err := h.queries.GetPOSImportLinesForReversal(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris import")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	// Reverse the import: mirror of the confirm posting, with each line's
	// direction taken from its line_type. Rebuilt from pos_import_lines rather
	// than from the journal so that imports confirmed before the journal existed
	// still reverse correctly.
	var reversal []service.Line
	var net int64
	for _, line := range lines {
		if line.LineType == "discount" {
			continue
		}
		var l service.Line
		switch line.LineType {
		case "cash", "expense":
			l = service.Cr(uuidFromPg(line.AccountID), line.Amount)
		default: // revenue
			l = service.Dr(uuidFromPg(line.AccountID), line.Amount)
		}
		reversal = append(reversal, l)
		net += l.Amount
	}

	if net != 0 {
		suspense, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: service.SuspenseAccountNumber, Valid: true})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "akun sementara tidak ditemukan")
			return
		}
		reversal = append(reversal,
			service.Dr(suspense.ID.Bytes, -net).WithMemo("selisih pembalikan impor POS"))
	}

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        time.Now(),
		SourceType:  service.SourcePOSImport,
		SourceID:    id,
		Description: "Pembalikan impor POS (dihapus)",
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines:       reversal,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembalikan impor POS")
		return
	}

	// Cascade delete via pos_imports (lines deleted by FK CASCADE).
	if err := qtx.DeletePOSImport(ctx, pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus import")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus import")
		return
	}

	desc := ""
	if imp.Description.Valid {
		desc = imp.Description.String
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:     userID,
		Username:   username,
		Action:     "DELETE",
		EntityType: "POSImport",
		EntityID:   id,
		Description: fmt.Sprintf("Hapus POS Import %s (%s): %d — saldo akun dikembalikan",
			imp.Date.Time.Format("2006-01-02"), desc, imp.TotalAmount),
	})

	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
