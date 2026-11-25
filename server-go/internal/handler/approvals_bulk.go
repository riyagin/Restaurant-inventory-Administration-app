package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Bulk approval of the Persetujuan queue (leave, overtime, shift changes,
// attendance permits) — manager only.
//
// Every item goes through the same per-type approval code as the single
// buttons, so quota checks, attendance rebuilds and the activity log are
// identical. Two steps:
//
//   - preview runs every approval inside ONE transaction, a savepoint per item,
//     and rolls the whole thing back. Later items see the quota earlier ones
//     used, so the preview predicts exactly what the real run will do.
//   - approve takes the previewed ids and approves each in its own
//     transaction, in the order given, so one failure never blocks the rest.
//     The client sends them in chunks to stay well inside the proxy timeout.
//
// Backdated requests are skipped unless include_backdated is set; when they
// are included the activity log still names them MUNDUR TANGGAL, and every
// bulk decision is logged with "— persetujuan massal".
type BulkApprovalHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	leave   *LeaveHandler
	shift   *ShiftHandler
}

func NewBulkApprovalHandler(pool *pgxpool.Pool, queries *db.Queries, leave *LeaveHandler, shift *ShiftHandler) *BulkApprovalHandler {
	return &BulkApprovalHandler{pool: pool, queries: queries, leave: leave, shift: shift}
}

var bulkApprovalKinds = []string{"leave", "overtime", "shift", "permission"}

const (
	bulkApprovalMaxPreview = 2000
	bulkApprovalMaxChunk   = 100
	bulkApprovalLogSuffix  = " — persetujuan massal"
)

type bulkApprovalItem struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	EmployeeName string `json:"employee_name"`
	EmployeeCode string `json:"employee_code"`
	Date         string `json:"date"`
	EndDate      string `json:"end_date"`
	Detail       string `json:"detail"`
	IsBackdated  bool   `json:"is_backdated"`
	// Outcome
	Status string `json:"status"` // ok | failed | skipped
	Error  string `json:"error,omitempty"`
}

type bulkApprovalSummary struct {
	Total   int                       `json:"total"`
	OK      int                       `json:"ok"`
	Failed  int                       `json:"failed"`
	Skipped int                       `json:"skipped"`
	ByType  map[string]map[string]int `json:"by_type"`
}

func summarize(items []bulkApprovalItem) bulkApprovalSummary {
	s := bulkApprovalSummary{Total: len(items), ByType: map[string]map[string]int{}}
	for _, it := range items {
		if s.ByType[it.Type] == nil {
			s.ByType[it.Type] = map[string]int{"ok": 0, "failed": 0, "skipped": 0}
		}
		s.ByType[it.Type][it.Status]++
		switch it.Status {
		case "ok":
			s.OK++
		case "failed":
			s.Failed++
		case "skipped":
			s.Skipped++
		}
	}
	return s
}

// listPending returns pending items oldest first (the order both steps use).
// ids, when non-nil, restricts the list to those request ids.
func (h *BulkApprovalHandler) listPending(ctx context.Context, q db.DBTX, kinds []string, branchID pgtype.UUID, ids []string) ([]bulkApprovalItem, error) {
	var idFilter any
	if ids != nil {
		idFilter = ids
	}
	rows, err := q.Query(ctx, `
		SELECT kind, id, full_name, code, to_char(d, 'YYYY-MM-DD'), to_char(d2, 'YYYY-MM-DD'), detail, is_backdated
		FROM (
			SELECT 'leave' AS kind, lr.id::text AS id, e.full_name, COALESCE(e.employee_code, '') AS code,
			       lr.start_date AS d, lr.end_date AS d2,
			       lt.name || ' · ' || lr.day_count || ' hari' AS detail, lr.is_backdated, lr.created_at AS created, e.branch_id
			FROM leave_requests lr
			JOIN employees e ON e.id = lr.employee_id
			JOIN leave_types lt ON lt.id = lr.leave_type_id
			WHERE lr.status = 'pending'
			UNION ALL
			SELECT 'overtime', o.id::text, e.full_name, COALESCE(e.employee_code, ''), o.date, o.date,
			       'Lembur ' || to_char(o.hours, 'FM999990.0') || ' jam', false, o.created_at, e.branch_id
			FROM overtime_requests o
			JOIN employees e ON e.id = o.employee_id
			WHERE o.status = 'pending'
			UNION ALL
			SELECT 'shift', c.id::text, e.full_name, COALESCE(e.employee_code, ''), c.date, c.date,
			       'Shift → ' || COALESCE(sd.code, 'tanpa shift'), false, c.requested_at, e.branch_id
			FROM shift_change_requests c
			JOIN employees e ON e.id = c.employee_id
			LEFT JOIN shift_definitions sd ON sd.id = c.shift_id
			WHERE c.status = 'pending'
			UNION ALL
			SELECT 'permission', p.id::text, e.full_name, COALESCE(e.employee_code, ''), p.date, p.date,
			       CASE p.type WHEN 'late_arrival' THEN 'Izin Masuk Siang'
			                   WHEN 'early_departure' THEN 'Izin Pulang Awal'
			                   ELSE 'Dinas Luar' END,
			       p.is_backdated, p.created_at, e.branch_id
			FROM attendance_permissions p
			JOIN employees e ON e.id = p.employee_id
			WHERE p.status = 'pending'
		) q
		WHERE kind = ANY($1::text[])
		  AND ($2::uuid IS NULL OR branch_id = $2)
		  AND ($3::text[] IS NULL OR id = ANY($3::text[]))
		ORDER BY d, created, kind, id
		LIMIT $4`, kinds, branchID, idFilter, bulkApprovalMaxPreview)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []bulkApprovalItem
	for rows.Next() {
		var it bulkApprovalItem
		if err := rows.Scan(&it.Type, &it.ID, &it.EmployeeName, &it.EmployeeCode, &it.Date, &it.EndDate, &it.Detail, &it.IsBackdated); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// approveItem approves one item on tx (the caller owns the transaction).
func (h *BulkApprovalHandler) approveItem(ctx context.Context, tx pgx.Tx, it bulkApprovalItem, note string) error {
	id, err := parseUUID(it.ID)
	if err != nil {
		return errBulkInvalidID
	}
	qtx := h.queries.WithTx(tx)
	pgNote := pgtype.Text{String: note, Valid: note != ""}

	switch it.Type {
	case "leave":
		_, err := h.leave.approveInTx(ctx, qtx, id, pgNote, leaveApproveBulkWithBackdated)
		return err

	case "overtime":
		deciderID := middleware.UserIDFromCtx(ctx)
		row, err := qtx.ApproveOvertimeRequest(ctx, &db.ApproveOvertimeRequestParams{
			ID:           pgtype.UUID{Bytes: id, Valid: true},
			DecidedBy:    pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}},
			DecisionNote: pgNote,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errBulkNotPending
			}
			return err
		}
		_ = service.LogActivity(ctx, qtx, service.LogParams{
			UserID:      deciderID,
			Username:    middleware.UsernameFromCtx(ctx),
			Action:      "UPDATE",
			EntityType:  "overtime_request",
			EntityID:    row.ID.Bytes,
			Description: "Menyetujui permintaan lembur karyawan" + bulkApprovalLogSuffix,
		})
		return nil

	case "permission":
		before, err := qtx.GetAttendancePermission(ctx, pgtype.UUID{Bytes: id, Valid: true})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errBulkPermissionNotFound
			}
			return err
		}
		_, err = decidePermissionInTx(ctx, qtx, "approve", before, note, bulkApprovalLogSuffix)
		return err

	case "shift":
		_, err := h.shift.decideShiftChangeInTx(ctx, tx, id, note, bulkApprovalLogSuffix, h.shift.approveShiftChangeFn)
		return err
	}
	return errBulkUnknownKind
}

var (
	errBulkInvalidID          = errors.New("ID tidak valid")
	errBulkNotPending         = errors.New("hanya pengajuan berstatus menunggu yang dapat disetujui")
	errBulkPermissionNotFound = errors.New("izin tidak ditemukan")
	errBulkUnknownKind        = errors.New("jenis pengajuan tidak dikenal")
)

// bulkItemError turns an approval error into the sentence shown in the list.
// Rule violations carry their own Indonesian message; anything else is logged
// and reported generically.
func bulkItemError(it bulkApprovalItem, err error) string {
	var rule *errShiftRule
	var perm errPermissionState
	switch {
	case errors.As(err, &rule):
		return rule.msg
	case errors.As(err, &perm):
		return string(perm)
	case errors.Is(err, service.ErrQuotaExceeded),
		errors.Is(err, errLeaveRequestNotFound),
		errors.Is(err, errLeaveRequestNotPending),
		errors.Is(err, errShiftRequestNotFound),
		errors.Is(err, errShiftRequestNotPending),
		errors.Is(err, errBulkInvalidID),
		errors.Is(err, errBulkNotPending),
		errors.Is(err, errBulkPermissionNotFound),
		errors.Is(err, errBulkUnknownKind):
		return err.Error()
	}
	log.Printf("bulk approval %s %s: %v", it.Type, it.ID, err)
	return "gagal memproses pengajuan (lihat log server)"
}

type bulkApprovalPreviewBody struct {
	Types            []string `json:"types"`
	BranchID         string   `json:"branch_id"`
	IncludeBackdated bool     `json:"include_backdated"`
}

func (b *bulkApprovalPreviewBody) kinds() ([]string, error) {
	if len(b.Types) == 0 {
		return bulkApprovalKinds, nil
	}
	for _, t := range b.Types {
		ok := false
		for _, k := range bulkApprovalKinds {
			ok = ok || t == k
		}
		if !ok {
			return nil, errors.New("jenis pengajuan tidak dikenal: " + t)
		}
	}
	return b.Types, nil
}

func bulkBranchFilter(s string) (pgtype.UUID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.UUID{}, nil
	}
	id, err := parseUUID(s)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

// Preview — POST /api/hr/approvals/bulk-preview (manager only). Changes nothing.
func (h *BulkApprovalHandler) Preview(w http.ResponseWriter, r *http.Request) {
	var body bulkApprovalPreviewBody
	_ = parseBody(r, &body)
	kinds, err := body.kinds()
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	branchID, err := bulkBranchFilter(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "cabang tidak valid")
		return
	}

	// Stay inside the proxy's 60 s read timeout.
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(context.Background()) // always: the preview never commits

	items, err := h.listPending(ctx, tx, kinds, branchID, nil)
	if err != nil {
		log.Printf("bulk approval preview list: %v", err)
		respondError(w, http.StatusInternalServerError, "gagal memuat antrian persetujuan")
		return
	}

	for i := range items {
		it := &items[i]
		if it.IsBackdated && !body.IncludeBackdated {
			it.Status, it.Error = "skipped", "mundur tanggal — tidak disertakan"
			continue
		}
		sp, err := tx.Begin(ctx) // SAVEPOINT
		if err == nil {
			err = h.approveItem(ctx, sp, *it, "")
			if err != nil {
				_ = sp.Rollback(ctx)
			} else {
				err = sp.Commit(ctx) // RELEASE: later items see this one's effect
			}
		}
		if ctx.Err() != nil {
			respondError(w, http.StatusServiceUnavailable,
				"pratinjau terlalu lama — saring per jenis atau cabang lalu coba lagi")
			return
		}
		if err != nil {
			it.Status, it.Error = "failed", bulkItemError(*it, err)
			continue
		}
		it.Status = "ok"
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"summary":   summarize(items),
		"truncated": len(items) >= bulkApprovalMaxPreview,
	})
}

type bulkApprovalApproveBody struct {
	Items []struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"items"`
	Note             string `json:"note"`
	IncludeBackdated bool   `json:"include_backdated"`
}

// Approve — POST /api/hr/approvals/bulk-approve (manager only). Approves the
// given items in the order given, one transaction each.
func (h *BulkApprovalHandler) Approve(w http.ResponseWriter, r *http.Request) {
	var body bulkApprovalApproveBody
	if err := parseBody(r, &body); err != nil || len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "daftar pengajuan wajib diisi")
		return
	}
	if len(body.Items) > bulkApprovalMaxChunk {
		respondError(w, http.StatusBadRequest, "maksimal 100 pengajuan per permintaan")
		return
	}
	note := strings.TrimSpace(body.Note)
	ctx := r.Context()

	// Re-read the items from the database: their backdated flag and pending
	// state are taken from there, never from the client.
	ids := make([]string, 0, len(body.Items))
	for _, it := range body.Items {
		ids = append(ids, it.ID)
	}
	current, err := h.listPending(ctx, h.pool, bulkApprovalKinds, pgtype.UUID{}, ids)
	if err != nil {
		log.Printf("bulk approval list: %v", err)
		respondError(w, http.StatusInternalServerError, "gagal memuat pengajuan")
		return
	}
	byKey := make(map[string]bulkApprovalItem, len(current))
	for _, it := range current {
		byKey[it.Type+":"+it.ID] = it
	}

	results := make([]bulkApprovalItem, 0, len(body.Items))
	for _, req := range body.Items {
		it, ok := byKey[req.Type+":"+req.ID]
		if !ok {
			results = append(results, bulkApprovalItem{Type: req.Type, ID: req.ID, Status: "failed",
				Error: "sudah tidak menunggu persetujuan"})
			continue
		}
		if it.IsBackdated && !body.IncludeBackdated {
			it.Status, it.Error = "skipped", "mundur tanggal — tidak disertakan"
			results = append(results, it)
			continue
		}
		err := func() error {
			tx, err := h.pool.Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			if err := h.approveItem(ctx, tx, it, note); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}()
		if err != nil {
			it.Status, it.Error = "failed", bulkItemError(it, err)
		} else {
			it.Status = "ok"
		}
		results = append(results, it)
	}

	respondJSON(w, http.StatusOK, map[string]any{"results": results, "summary": summarize(results)})
}
