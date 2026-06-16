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
// Auto-detect logic per employee per day:
//   - Earliest punch before 12:00 → check_in
//   - Latest punch at/after 12:00  → check_out
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

	// ── Phase 2: group punches by (employeeID, date), resolve check_in/out ───
	type dayKey struct {
		empID [16]byte
		date  string // "2006-01-02"
	}
	type resolvedGroup struct {
		info     *empInfo
		checkIn  *time.Time // earliest punch before 12:00
		checkOut *time.Time // latest punch at/after 12:00
	}

	groups := map[dayKey]*resolvedGroup{}
	var groupOrder []dayKey // preserve iteration order

	for _, p := range punches {
		info := empCache[p.EmployeeCode]
		if info == nil {
			continue
		}
		dateStr := p.Timestamp.Format("2006-01-02")
		key := dayKey{empID: info.id.Bytes, date: dateStr}

		g, exists := groups[key]
		if !exists {
			g = &resolvedGroup{info: info}
			groups[key] = g
			groupOrder = append(groupOrder, key)
		}

		ts := p.Timestamp
		if ts.Hour() < 12 {
			if g.checkIn == nil || ts.Before(*g.checkIn) {
				tsCopy := ts
				g.checkIn = &tsCopy
			}
		} else {
			if g.checkOut == nil || ts.After(*g.checkOut) {
				tsCopy := ts
				g.checkOut = &tsCopy
			}
		}
	}

	// ── Phase 3: apply to attendance records inside a transaction ────────────
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	schedCache := map[string]service.Schedule{}
	matched := 0

	for _, key := range groupOrder {
		g := groups[key]
		if g.checkIn == nil && g.checkOut == nil {
			continue
		}

		dateOnly, _ := time.Parse("2006-01-02", key.date)
		pgDate := pgtype.Date{Time: dateOnly, Valid: true}

		bkey := string(g.info.branchID.Bytes[:])
		sched, has := schedCache[bkey]
		if !has {
			ws, serr := qtx.GetWorkScheduleByBranch(ctx, g.info.branchID)
			if serr != nil || ws == nil {
				sched = service.DefaultSchedule()
			} else {
				sched = service.ScheduleFromRow(ws)
			}
			schedCache[bkey] = sched
		}

		existing, eerr := qtx.GetAttendanceRecordByEmployeeDate(ctx, &db.GetAttendanceRecordByEmployeeDateParams{
			EmployeeID: g.info.id,
			Date:       pgDate,
		})
		hasExisting := eerr == nil

		var state *service.AttendanceState
		if hasExisting {
			state = service.StateFromRecord(existing)
		} else {
			state = service.EmptyState()
		}

		if g.checkIn != nil {
			service.MergeAttendanceEvent(state, service.AttendanceEvent{
				Timestamp: *g.checkIn,
				Source:    "fingerprint",
				Direction: "check_in",
			})
		}
		if g.checkOut != nil {
			service.MergeAttendanceEvent(state, service.AttendanceEvent{
				Timestamp: *g.checkOut,
				Source:    "fingerprint",
				Direction: "check_out",
			})
		}
		service.ComputeAnomalies(state, sched, service.DayIsOver(dateOnly, sched, time.Now()))

		if hasExisting {
			params := db.UpdateAttendanceRecordParams{
				CheckInPhotoPath: existing.CheckInPhotoPath,
				DeviceID:         existing.DeviceID,
				Note:             existing.Note,
				ID:               existing.ID,
			}
			service.FillUpdateParams(&params, state)
			if _, uerr := qtx.UpdateAttendanceRecord(ctx, &params); uerr != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran sidik jari")
				return
			}
		} else {
			params := db.InsertAttendanceRecordParams{
				EmployeeID: g.info.id,
				Date:       pgDate,
			}
			service.FillInsertParams(&params, state)
			if _, ierr := qtx.InsertAttendanceRecord(ctx, &params); ierr != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran sidik jari")
				return
			}
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
