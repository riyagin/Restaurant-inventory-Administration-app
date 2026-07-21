package handler

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/xuri/excelize/v2"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Excel import/export of performance policies. Single-step (no preview batch) —
// policies are few and simple. The export layout round-trips: re-uploading an
// exported file updates the matching policies by id, while blank-id rows insert.

const policySheet = "Kebijakan"

// policyExportHeaders is the fixed column order of the export/import sheet.
var policyExportHeaders = []string{
	"id", "name", "rule_type", "threshold_minutes", "points", "max_occurrences_per_month", "is_active",
}

// ruleTypeLabels maps each rule_type code to its Indonesian label (mirrors the
// frontend). Used to annotate the export and to accept labels on import.
var ruleTypeLabels = map[string]string{
	"late":             "Terlambat",
	"early_leave":      "Pulang Awal",
	"missing_checkout": "Tidak Absen Pulang",
	"missing_checkin":  "Tidak Absen Masuk",
	"no_punch":         "Tidak Absen Masuk & Pulang",
	"half_day_late":    "Setengah Hari (Datang Siang)",
	"half_day_early":   "Setengah Hari (Pulang Awal)",
	"absent_no_leave":  "Absen Tanpa Cuti",
	"manual":           "Manual",
}

// labelToRuleType is the reverse of ruleTypeLabels (lowercased) so imports may
// use either the code or the Indonesian label in the rule_type column.
var labelToRuleType = func() map[string]string {
	m := map[string]string{}
	for code, label := range ruleTypeLabels {
		m[strings.ToLower(label)] = code
	}
	return m
}()

// ExportPolicies — GET /api/hr/performance/policies/export
// Streams an .xlsx of every policy in the round-trippable "Kebijakan" layout,
// plus a "Petunjuk" sheet documenting the valid rule_type codes.
func (h *PerformanceHandler) ExportPolicies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	policies, err := h.queries.ListPerformancePolicies(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil kebijakan kinerja")
		return
	}

	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName(f.GetSheetName(0), policySheet)

	boldStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"D9E1F2"}, Pattern: 1},
	})
	for i, hcell := range policyExportHeaders {
		col, _ := excelize.ColumnNumberToName(i + 1)
		cell := fmt.Sprintf("%s1", col)
		f.SetCellStr(policySheet, cell, hcell)
		f.SetCellStyle(policySheet, cell, cell, boldStyle)
	}
	f.SetPanes(policySheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})

	rowIdx := 2
	for _, p := range policies {
		set := func(colIdx int, v string) {
			col, _ := excelize.ColumnNumberToName(colIdx)
			f.SetCellStr(policySheet, fmt.Sprintf("%s%d", col, rowIdx), v)
		}
		set(1, uuidToString(p.ID))
		set(2, p.Name)
		set(3, p.RuleType)
		if p.ThresholdMinutes.Valid {
			set(4, strconv.Itoa(int(p.ThresholdMinutes.Int32)))
		}
		set(5, strconv.Itoa(int(p.Points)))
		if p.MaxOccurrencesPerMonth.Valid {
			set(6, strconv.Itoa(int(p.MaxOccurrencesPerMonth.Int32)))
		}
		set(7, boolToYaTidak(p.IsActive))
		rowIdx++
	}
	f.SetColWidth(policySheet, "A", "A", 38)
	f.SetColWidth(policySheet, "B", "B", 40)
	f.SetColWidth(policySheet, "C", "C", 18)

	// Instructions sheet.
	const instr = "Petunjuk"
	f.NewSheet(instr)
	lines := []string{
		"PETUNJUK IMPOR/EKSPOR KEBIJAKAN KINERJA",
		"",
		"1. Isi data pada sheet \"Kebijakan\". Baris pertama adalah header — jangan diubah.",
		"2. Kolom id: biarkan KOSONG untuk membuat kebijakan baru. Jika diisi (dari hasil ekspor), kebijakan dengan id tersebut akan DIPERBARUI.",
		"3. Kolom wajib: name, rule_type, points.",
		"4. points harus bilangan bulat > 0 (jumlah poin yang dikurangi dari skor 100).",
		"5. threshold_minutes hanya berlaku untuk rule_type 'late' dan 'early_leave' (diabaikan untuk lainnya).",
		"6. max_occurrences_per_month opsional (kosong = tanpa batas).",
		"7. is_active: isi 'ya' atau 'tidak' (kosong dianggap 'ya').",
		"8. rule_type boleh memakai KODE atau label Indonesianya.",
		"",
		"DAFTAR rule_type YANG VALID (kode = label):",
	}
	for _, code := range []string{
		"late", "early_leave", "missing_checkout", "missing_checkin", "no_punch",
		"half_day_late", "half_day_early", "absent_no_leave", "manual",
	} {
		lines = append(lines, "  - "+code+"  =  "+ruleTypeLabels[code])
	}
	for i, line := range lines {
		f.SetCellStr(instr, fmt.Sprintf("A%d", i+1), line)
	}
	f.SetColWidth(instr, "A", "A", 100)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat file ekspor")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="kebijakan-kinerja.xlsx"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// parsedPolicyRow is one validated import row ready to upsert.
type parsedPolicyRow struct {
	id     pgtype.UUID // valid → update
	params db.CreatePerformancePolicyParams
}

// ImportPolicies — POST /api/hr/performance/policies/import
// Parses the uploaded .xlsx, validates every row, and upserts all rows in one
// transaction (all-or-nothing). Rows with an id update; blank-id rows insert.
func (h *PerformanceHandler) ImportPolicies(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "gagal membaca form (maks 10 MB)")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "tidak ada file yang diunggah")
		return
	}
	defer file.Close()

	f, err := excelize.OpenReader(file)
	if err != nil {
		respondError(w, http.StatusBadRequest, "file Excel tidak valid")
		return
	}
	defer f.Close()

	// Prefer the "Kebijakan" sheet; fall back to the first sheet.
	sheet := policySheet
	if idx, ierr := f.GetSheetIndex(policySheet); ierr != nil || idx < 0 {
		sheet = f.GetSheetName(0)
	}
	rows, err := f.GetRows(sheet)
	if err != nil || len(rows) < 2 {
		respondError(w, http.StatusBadRequest, "sheet kosong atau tidak ditemukan")
		return
	}

	// Map header name → column index (case-insensitive), so column order is flexible.
	colOf := map[string]int{}
	for i, h := range rows[0] {
		colOf[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for _, req := range []string{"name", "rule_type", "points"} {
		if _, ok := colOf[req]; !ok {
			respondError(w, http.StatusBadRequest, "kolom wajib tidak ditemukan: "+req)
			return
		}
	}

	// Existing ids so id references can be validated before writing.
	existing, err := h.queries.ListPerformancePolicies(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memuat kebijakan")
		return
	}
	existingIDs := map[string]bool{}
	for _, p := range existing {
		existingIDs[strings.ToLower(uuidToString(p.ID))] = true
	}

	cell := func(row []string, name string) string {
		idx, ok := colOf[name]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	var parsed []parsedPolicyRow
	var rowErrs []string
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		rowNo := i + 1 // 1-based, matching the spreadsheet

		name := cell(row, "name")
		ruleRaw := cell(row, "rule_type")
		pointsRaw := cell(row, "points")
		// Skip fully blank rows.
		if name == "" && ruleRaw == "" && pointsRaw == "" && cell(row, "id") == "" {
			continue
		}

		if name == "" {
			rowErrs = append(rowErrs, fmt.Sprintf("Baris %d: name wajib diisi", rowNo))
			continue
		}
		ruleType := normalizeRuleType(ruleRaw)
		if !validRuleType(ruleType) {
			rowErrs = append(rowErrs, fmt.Sprintf("Baris %d: rule_type '%s' tidak valid", rowNo, ruleRaw))
			continue
		}
		points, perr := strconv.Atoi(pointsRaw)
		if perr != nil || points <= 0 {
			rowErrs = append(rowErrs, fmt.Sprintf("Baris %d: points harus bilangan bulat > 0", rowNo))
			continue
		}

		var threshold pgtype.Int4
		if ruleType == "late" || ruleType == "early_leave" {
			if v := cell(row, "threshold_minutes"); v != "" {
				n, terr := strconv.Atoi(v)
				if terr != nil || n < 0 {
					rowErrs = append(rowErrs, fmt.Sprintf("Baris %d: threshold_minutes tidak valid", rowNo))
					continue
				}
				threshold = pgtype.Int4{Int32: int32(n), Valid: true}
			}
		}

		var maxOcc pgtype.Int4
		if v := cell(row, "max_occurrences_per_month"); v != "" {
			n, merr := strconv.Atoi(v)
			if merr != nil || n < 0 {
				rowErrs = append(rowErrs, fmt.Sprintf("Baris %d: max_occurrences_per_month tidak valid", rowNo))
				continue
			}
			maxOcc = pgtype.Int4{Int32: int32(n), Valid: true}
		}

		isActive := parseYaTidak(cell(row, "is_active"))

		pr := parsedPolicyRow{
			params: db.CreatePerformancePolicyParams{
				Name:                   name,
				RuleType:               ruleType,
				ThresholdMinutes:       threshold,
				Points:                 int32(points),
				MaxOccurrencesPerMonth: maxOcc,
				IsActive:               isActive,
			},
		}
		if idStr := cell(row, "id"); idStr != "" {
			idb, ierr := parseUUID(idStr)
			if ierr != nil || !existingIDs[strings.ToLower(idStr)] {
				rowErrs = append(rowErrs, fmt.Sprintf("Baris %d: id '%s' tidak ditemukan", rowNo, idStr))
				continue
			}
			pr.id = pgtype.UUID{Bytes: idb, Valid: true}
		}
		parsed = append(parsed, pr)
	}

	if len(rowErrs) > 0 {
		respondJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "terdapat kesalahan pada file; tidak ada yang diimpor",
			"errors": rowErrs,
		})
		return
	}
	if len(parsed) == 0 {
		respondError(w, http.StatusBadRequest, "tidak ada baris untuk diimpor")
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	var created, updated int
	for _, pr := range parsed {
		if pr.id.Valid {
			if _, err := qtx.UpdatePerformancePolicy(ctx, &db.UpdatePerformancePolicyParams{
				Name:                   pr.params.Name,
				RuleType:               pr.params.RuleType,
				ThresholdMinutes:       pr.params.ThresholdMinutes,
				Points:                 pr.params.Points,
				MaxOccurrencesPerMonth: pr.params.MaxOccurrencesPerMonth,
				IsActive:               pr.params.IsActive,
				ID:                     pr.id,
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui kebijakan: "+pr.params.Name)
				return
			}
			updated++
		} else {
			p := pr.params
			if _, err := qtx.CreatePerformancePolicy(ctx, &p); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membuat kebijakan: "+pr.params.Name)
				return
			}
			created++
		}
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "performance_policy",
		Description: fmt.Sprintf("Impor kebijakan kinerja: %d dibuat, %d diperbarui", created, updated),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan impor")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"created": created, "updated": updated})
}

// ── small helpers ────────────────────────────────────────────────────────────

// normalizeRuleType accepts either a rule_type code or its Indonesian label and
// returns the canonical code (lowercased). Unknown values pass through unchanged
// so validRuleType can reject them with the original text.
func normalizeRuleType(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if validRuleType(s) {
		return s
	}
	if code, ok := labelToRuleType[s]; ok {
		return code
	}
	return s
}

func boolToYaTidak(b bool) string {
	if b {
		return "ya"
	}
	return "tidak"
}

// parseYaTidak interprets an is_active cell. Blank → true (active by default).
func parseYaTidak(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "ya", "yes", "true", "1", "aktif", "y":
		return true
	default:
		return false
	}
}

// uuidToString renders a pgtype.UUID as canonical hyphenated text ("" when invalid).
func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
