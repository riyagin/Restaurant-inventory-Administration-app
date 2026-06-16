package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type HRImportHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewHRImportHandler(pool *pgxpool.Pool, queries *db.Queries) *HRImportHandler {
	return &HRImportHandler{pool: pool, queries: queries}
}

// loadRefData builds the reference sets (positions, branches, active wage
// components, existing employee codes/name+dob) the importer needs.
func (h *HRImportHandler) loadRefData(r *http.Request) (service.HRImportRefData, []*db.WageComponent, error) {
	ctx := r.Context()
	ref := service.HRImportRefData{
		Positions:       map[string]pgtype.UUID{},
		Branches:        map[string]pgtype.UUID{},
		ExistingCodes:   map[string]bool{},
		ExistingNameDob: map[string]bool{},
		Components:      map[string]*db.WageComponent{},
	}

	positions, err := h.queries.ListPositions(ctx)
	if err != nil {
		return ref, nil, err
	}
	for _, p := range positions {
		ref.Positions[strings.ToLower(p.Name)] = p.ID
	}

	branches, err := h.queries.ListBranches(ctx)
	if err != nil {
		return ref, nil, err
	}
	for _, b := range branches {
		ref.Branches[strings.ToLower(b.Name)] = b.ID
	}

	components, err := h.queries.ListActiveWageComponents(ctx)
	if err != nil {
		return ref, nil, err
	}
	// Components arrive ordered by (type, name) from the query; keep that order
	// for the template/parser column layout.
	sort.SliceStable(components, func(i, j int) bool {
		if components[i].Type != components[j].Type {
			return components[i].Type < components[j].Type
		}
		return components[i].Name < components[j].Name
	})
	for _, c := range components {
		ref.Components[c.Name] = c
	}

	// Existing employee codes + name|dob and the max code sequence.
	rows, err := h.pool.Query(ctx, `SELECT employee_code, full_name, dob FROM employees`)
	if err != nil {
		return ref, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code, fullName string
		var dob pgtype.Date
		if err := rows.Scan(&code, &fullName, &dob); err != nil {
			return ref, nil, err
		}
		ref.ExistingCodes[code] = true
		if dob.Valid {
			key := strings.ToLower(fullName) + "|" + dob.Time.Format("2006-01-02")
			ref.ExistingNameDob[key] = true
		}
	}
	if err := rows.Err(); err != nil {
		return ref, nil, err
	}

	maxSeq, err := h.queries.GetMaxEmployeeCodeSeq(ctx)
	if err != nil {
		return ref, nil, err
	}
	ref.MaxEmployeeCodeSeq = maxSeq

	return ref, components, nil
}

// Template — GET /api/hr/import/template
// Builds and streams a fresh .xlsx with the fixed columns + one column per
// active wage component, plus an Indonesian instructions sheet.
func (h *HRImportHandler) Template(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	positions, err := h.queries.ListPositions(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data jabatan")
		return
	}
	branches, err := h.queries.ListBranches(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data cabang")
		return
	}
	components, err := h.queries.ListActiveWageComponents(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil komponen gaji")
		return
	}
	sort.SliceStable(components, func(i, j int) bool {
		if components[i].Type != components[j].Type {
			return components[i].Type < components[j].Type
		}
		return components[i].Name < components[j].Name
	})

	f := excelize.NewFile()
	defer f.Close()

	// Sheet "Karyawan"
	const sheet = "Karyawan"
	f.SetSheetName(f.GetSheetName(0), sheet)

	headers := append([]string{}, service.HRImportFixedHeaders...)
	for _, c := range components {
		headers = append(headers, service.ComponentColumnHeader(c))
	}

	boldStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"D9E1F2"}, Pattern: 1},
	})

	for i, hcell := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		cell := fmt.Sprintf("%s1", col)
		f.SetCellStr(sheet, cell, hcell)
		f.SetCellStyle(sheet, cell, cell, boldStyle)
	}

	// Freeze the header row.
	f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})

	// One example row.
	example := []string{
		"", "Budi Santoso", "1995-08-17", "2024-01-15",
	}
	if len(positions) > 0 {
		example = append(example, positions[0].Name)
	} else {
		example = append(example, "Barista")
	}
	if len(branches) > 0 {
		example = append(example, branches[0].Name)
	} else {
		example = append(example, "Pusat")
	}
	example = append(example,
		"081234567890", "budi@contoh.id", "Jl. Contoh No. 1", "3201234567890001",
		"BCA", "1234567890", "Budi Santoso", "5000000", "26", "2024-01-15",
	)
	for range components {
		example = append(example, "0")
	}
	for i, val := range example {
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetCellStr(sheet, fmt.Sprintf("%s2", col), val)
	}

	// Sheet "Petunjuk"
	const instr = "Petunjuk"
	f.NewSheet(instr)
	lines := []string{
		"PETUNJUK PENGISIAN IMPOR KARYAWAN",
		"",
		"1. Isi data pada sheet \"Karyawan\". Baris pertama adalah header — jangan diubah.",
		"2. Format tanggal harus YYYY-MM-DD (contoh: 2024-01-15) untuk dob, join_date, dan effective_date.",
		"3. Kolom wajib: full_name, join_date, position, branch, base_salary, working_days_per_month, effective_date.",
		"4. Kosongkan kolom employee_code agar kode dibuat otomatis (EMP-0001, EMP-0002, ...).",
		"5. position dan branch harus PERSIS cocok (huruf besar/kecil diabaikan) dengan nama yang sudah ada.",
		"6. Semua nominal (base_salary dan komponen) ditulis dalam RUPIAH PENUH, bilangan bulat (tanpa titik desimal, contoh: 5000000).",
		"7. working_days_per_month antara 1 sampai 31.",
		"8. Kolom komponen ([Tunjangan]/[Bonus]/[Potongan]) opsional — kosongkan jika tidak berlaku.",
		"",
		"DAFTAR JABATAN (position) YANG TERSEDIA:",
	}
	if len(positions) == 0 {
		lines = append(lines, "  (belum ada jabatan — buat dulu di menu Jabatan)")
	}
	for _, p := range positions {
		lines = append(lines, "  - "+p.Name)
	}
	lines = append(lines, "", "DAFTAR CABANG (branch) YANG TERSEDIA:")
	if len(branches) == 0 {
		lines = append(lines, "  (belum ada cabang)")
	}
	for _, b := range branches {
		lines = append(lines, "  - "+b.Name)
	}
	for i, line := range lines {
		f.SetCellStr(instr, fmt.Sprintf("A%d", i+1), line)
	}
	f.SetColWidth(instr, "A", "A", 90)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat template")
		return
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="template-impor-karyawan.xlsx"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// Parse — POST /api/hr/import/parse
// Validates every row (no DB writes), persists the preview as a batch, and
// returns the preview JSON (with the batch id) for the user to review.
func (h *HRImportHandler) Parse(w http.ResponseWriter, r *http.Request) {
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

	ref, components, err := h.loadRefData(r)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memuat data referensi")
		return
	}

	preview, err := service.ParseHRImportExcel(file, header.Filename, components, ref)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	payload, err := json.Marshal(preview)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyiapkan data")
		return
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)

	batch, err := h.queries.CreateHRImportBatch(ctx, &db.CreateHRImportBatchParams{
		UploadedBy: pgtype.UUID{Bytes: userID, Valid: userID.String() != "00000000-0000-0000-0000-000000000000"},
		Filename:   header.Filename,
		Payload:    payload,
		RowCount:   int32(preview.TotalRows),
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan batch impor")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"batch_id": batch.ID,
		"preview":  preview,
	})
}

// Confirm — POST /api/hr/import/confirm
// Loads the persisted batch and inserts every row in ONE transaction
// (all-or-nothing). Logs a single activity entry with the row count.
func (h *HRImportHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BatchID string `json:"batch_id"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	batchID, err := parseUUID(strings.TrimSpace(body.BatchID))
	if err != nil {
		respondError(w, http.StatusBadRequest, "batch_id tidak valid")
		return
	}

	ctx := r.Context()
	pgBatchID := pgtype.UUID{Bytes: batchID, Valid: true}

	batch, err := h.queries.GetHRImportBatch(ctx, pgBatchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "batch impor tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memuat batch impor")
		return
	}
	if batch.Status == "confirmed" {
		respondError(w, http.StatusBadRequest, "batch impor sudah dikonfirmasi sebelumnya")
		return
	}

	var preview service.HRImportPreview
	if err := json.Unmarshal(batch.Payload, &preview); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data batch")
		return
	}
	if preview.ErrorCount > 0 {
		respondError(w, http.StatusBadRequest, "masih ada baris dengan kesalahan; perbaiki file lalu unggah ulang")
		return
	}
	if preview.TotalRows == 0 {
		respondError(w, http.StatusBadRequest, "tidak ada baris untuk diimpor")
		return
	}

	// Rebuild reference data fresh from the DB for ID resolution.
	ref, _, err := h.loadRefData(r)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memuat data referensi")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	createdBy := pgtype.UUID{Bytes: userID, Valid: userID.String() != "00000000-0000-0000-0000-000000000000"}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	result, err := service.ConfirmHRImport(ctx, qtx, &preview, ref, createdBy)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := qtx.MarkHRImportBatchConfirmed(ctx, pgBatchID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menandai batch impor")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "hr_import",
		EntityID:    batchID,
		Description: fmt.Sprintf("Impor massal %d karyawan dari %s", result.EmployeesCreated, batch.Filename),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan impor")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"employees_created": result.EmployeesCreated,
	})
}
