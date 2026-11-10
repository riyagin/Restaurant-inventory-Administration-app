package handler

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Fingerprint import — two-phase like the prompt-03 Excel import.
//
// Phase 1 (parse): upload the device export, match employee_code against the
// roster, return matched/unmatched preview. Unmatched codes are reported, NOT
// fatal.
// Phase 2 (confirm): re-upload the SAME file; matched punches are merged into
// attendance records (fingerprint source — never overwrites face values).
//
// The placeholder generic-CSV parser is injected via h.parser; swapping in the
// real device format requires no change here (see FingerprintParser).

type fingerprintMatchedPunch struct {
	EmployeeCode string `json:"employee_code"`
	FullName     string `json:"full_name"`
	Timestamp    string `json:"timestamp"`
}

type fingerprintPreview struct {
	Filename       string                    `json:"filename"`
	TotalPunches   int                       `json:"total_punches"`
	MatchedCount   int                       `json:"matched_count"`
	UnmatchedCount int                       `json:"unmatched_count"`
	Matched        []fingerprintMatchedPunch `json:"matched"`
	UnmatchedCodes []string                  `json:"unmatched_codes"`
	RowErrors      []string                  `json:"row_errors"`
}

// FingerprintParse — POST /api/hr/attendance/fingerprint-import/parse
func (h *AttendanceHandler) FingerprintParse(w http.ResponseWriter, r *http.Request) {
	punches, rowErrors, filename, ok := h.readPunches(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	matchedSet := map[string]string{} // code -> full_name (cache)
	preview := fingerprintPreview{
		Filename:       filename,
		TotalPunches:   len(punches),
		Matched:        []fingerprintMatchedPunch{},
		UnmatchedCodes: []string{},
		RowErrors:      rowErrors,
	}
	unmatched := map[string]bool{}

	for _, p := range punches {
		name, seen := matchedSet[p.EmployeeCode]
		if !seen {
			emp, err := h.queries.GetEmployeeByCode(ctx, p.EmployeeCode)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					unmatched[p.EmployeeCode] = true
					continue
				}
				respondError(w, http.StatusInternalServerError, "gagal mencocokkan karyawan")
				return
			}
			name = emp.FullName
			matchedSet[p.EmployeeCode] = name
		}
		preview.Matched = append(preview.Matched, fingerprintMatchedPunch{
			EmployeeCode: p.EmployeeCode,
			FullName:     name,
			Timestamp:    p.Timestamp.Format(time.RFC3339),
		})
	}

	preview.MatchedCount = len(preview.Matched)
	for code := range unmatched {
		preview.UnmatchedCodes = append(preview.UnmatchedCodes, code)
	}
	sort.Strings(preview.UnmatchedCodes)
	preview.UnmatchedCount = len(preview.UnmatchedCodes)

	respondJSON(w, http.StatusOK, preview)
}

// FingerprintConfirm — POST /api/hr/attendance/fingerprint-import/confirm
// Re-uploads the same file and applies matched punches.
//
// Every punch is written to the punch log (idempotent, so re-importing the
// device's cumulative attlog.dat is safe) and assigned to the work day whose
// shift window holds it. Each touched day is then rebuilt from its punches: the
// first is the check-in, the last the check-out, by the employee's shift — see
// service/punches.go. Manual corrections and approved permits are kept.
func (h *AttendanceHandler) FingerprintConfirm(w http.ResponseWriter, r *http.Request) {
	punches, _, filename, ok := h.readPunches(w, r)
	if !ok {
		return
	}

	ctx := r.Context()

	// ── Phase 1: resolve employees (outside transaction) ─────────────────────
	type empInfo struct {
		id       pgtype.UUID
		branchID pgtype.UUID
	}
	empCache := map[string]*empInfo{} // code → info (nil = not found)

	for _, p := range punches {
		if _, seen := empCache[p.EmployeeCode]; seen {
			continue
		}
		emp, err := h.queries.GetEmployeeByCode(ctx, p.EmployeeCode)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				empCache[p.EmployeeCode] = nil
				continue
			}
			respondError(w, http.StatusInternalServerError, "gagal mencocokkan karyawan")
			return
		}
		empCache[p.EmployeeCode] = &empInfo{id: emp.ID, branchID: emp.BranchID}
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	branchSched := map[[16]byte]service.Schedule{}
	baseFor := func(branchID pgtype.UUID) service.Schedule {
		if s, ok := branchSched[branchID.Bytes]; ok {
			return s
		}
		s := service.DefaultSchedule()
		if ws, serr := qtx.GetWorkScheduleByBranch(ctx, branchID); serr == nil && ws != nil {
			s = service.ScheduleFromRow(ws)
		}
		branchSched[branchID.Bytes] = s
		return s
	}

	// ── Phase 2: log every punch on its work day ─────────────────────────────
	type dayKey struct {
		empID [16]byte
		date  time.Time
	}
	type touchedDay struct {
		empID pgtype.UUID
		date  time.Time
		sched service.Schedule
	}
	touched := map[dayKey]touchedDay{}
	var order []dayKey
	for _, p := range punches {
		info := empCache[p.EmployeeCode]
		if info == nil {
			continue
		}
		day, sched, lerr := service.LogPunch(ctx, qtx, info.id,
			service.Punch{At: p.Timestamp, Source: "fingerprint"}, baseFor(info.branchID), pgtype.UUID{})
		if lerr != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan punch sidik jari")
			return
		}
		k := dayKey{info.id.Bytes, day}
		if _, seen := touched[k]; !seen {
			touched[k] = touchedDay{info.id, day, sched}
			order = append(order, k)
		}
	}

	// ── Phase 3: rebuild each touched day from its punches ───────────────────
	matched := 0
	for _, k := range order {
		d := touched[k]
		if _, _, rerr := service.RebuildDay(ctx, qtx, d.empID, d.date, d.sched, service.RebuildOptions{}); rerr != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran sidik jari")
			return
		}
		matched++
	}

	userID := middleware.UserIDFromCtx(ctx)
	imp, err := qtx.CreateFingerprintImport(ctx, &db.CreateFingerprintImportParams{
		Filename:     pgtype.Text{String: filename, Valid: filename != ""},
		ImportedBy:   pgtype.UUID{Bytes: userID, Valid: userID.String() != "00000000-0000-0000-0000-000000000000"},
		RowCount:     pgtype.Int4{Int32: int32(len(punches)), Valid: true},
		MatchedCount: pgtype.Int4{Int32: int32(matched), Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat batch impor")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      userID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "fingerprint_import",
		EntityID:    imp.ID.Bytes,
		Description: fmt.Sprintf("Impor sidik jari dari %s: %d punch cocok", filename, matched),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan impor")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"total_punches": len(punches),
		"applied":       matched,
	})
}

// readPunches reads the uploaded file and runs it through the configured parser.
// Returns (punches, rowErrors, filename, ok). On error it writes the response.
func (h *AttendanceHandler) readPunches(w http.ResponseWriter, r *http.Request) ([]service.ParsedPunch, []string, string, bool) {
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "gagal membaca form (maks 20 MB)")
		return nil, nil, "", false
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "tidak ada file yang diunggah")
		return nil, nil, "", false
	}
	defer file.Close()

	punches, rowErrors, perr := h.parser.Parse(file)
	if perr != nil {
		respondError(w, http.StatusBadRequest, perr.Error())
		return nil, nil, "", false
	}
	return punches, rowErrors, header.Filename, true
}
