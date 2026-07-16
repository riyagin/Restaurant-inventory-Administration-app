package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type OvertimeHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewOvertimeHandler(pool *pgxpool.Pool, queries *db.Queries) *OvertimeHandler {
	return &OvertimeHandler{pool: pool, queries: queries}
}

type overtimeRequestBody struct {
	EmployeeID string  `json:"employee_id"`
	Date       string  `json:"date"`    // "YYYY-MM-DD"
	Hours      float64 `json:"hours"`
	Reason     string  `json:"reason"`
}

// List — GET /api/hr/overtime?month=YYYY-MM&employee_id=UUID&status=&branch_id=
// Accessible to admin and manager.
func (h *OvertimeHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	month := strings.TrimSpace(q.Get("month"))
	empIDStr := strings.TrimSpace(q.Get("employee_id"))
	status := strings.TrimSpace(q.Get("status"))
	branchIDStr := strings.TrimSpace(q.Get("branch_id"))

	type row struct {
		ID                string  `json:"id"`
		EmployeeID        string  `json:"employee_id"`
		EmployeeName      string  `json:"employee_name"`
		EmployeeCode      string  `json:"employee_code"`
		BranchID          *string `json:"branch_id"`
		BranchName        *string `json:"branch_name"`
		Date              string  `json:"date"`
		Hours             float64 `json:"hours"`
		Reason            *string `json:"reason"`
		Status            string  `json:"status"`
		DecisionNote      *string `json:"decision_note"`
		DecidedAt         *string `json:"decided_at"`
		DecidedByUsername *string `json:"decided_by_username"`
		CreatedByUsername *string `json:"created_by_username"`
		CreatedAt         string  `json:"created_at"`
	}

	sql := `
SELECT o.id, o.employee_id, e.full_name AS employee_name, e.employee_code,
       e.branch_id, b.name AS branch_name,
       o.date, o.hours::float8, o.reason, o.status, o.decision_note,
       o.decided_at, du.username AS decided_by_username,
       cu.username AS created_by_username, o.created_at
FROM overtime_requests o
JOIN employees e ON e.id = o.employee_id
LEFT JOIN branches b ON b.id = e.branch_id
LEFT JOIN users cu ON cu.id = o.created_by
LEFT JOIN users du ON du.id = o.decided_by
WHERE true`
	args := []any{}

	if month != "" {
		if t, err := parseMonth(month); err == nil {
			end := time.Date(t.Year(), t.Month()+1, 0, 23, 59, 59, 0, time.UTC)
			args = append(args, t, end)
			sql += ` AND o.date >= $` + itoa(len(args)-1) + ` AND o.date <= $` + itoa(len(args))
		}
	}
	if empIDStr != "" {
		empID, err := parseUUID(empIDStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "employee_id tidak valid")
			return
		}
		args = append(args, pgtype.UUID{Bytes: empID, Valid: true})
		sql += ` AND o.employee_id = $` + itoa(len(args))
	}
	if status != "" {
		args = append(args, status)
		sql += ` AND o.status = $` + itoa(len(args))
	}
	if branchIDStr != "" {
		branchID, err := parseUUID(branchIDStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		args = append(args, pgtype.UUID{Bytes: branchID, Valid: true})
		sql += ` AND e.branch_id = $` + itoa(len(args))
	}

	sql += ` ORDER BY o.date DESC, e.full_name`

	rows, err := h.pool.Query(r.Context(), sql, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data lembur")
		return
	}
	defer rows.Close()

	result := []row{}
	for rows.Next() {
		var item row
		var branchName, decisionNote pgtype.Text
		var dateVal pgtype.Date
		var decidedAt, createdAt pgtype.Timestamptz
		if err := rows.Scan(
			&item.ID, &item.EmployeeID, &item.EmployeeName, &item.EmployeeCode,
			&item.BranchID, &branchName, &dateVal, &item.Hours, &item.Reason,
			&item.Status, &decisionNote, &decidedAt, &item.DecidedByUsername,
			&item.CreatedByUsername, &createdAt,
		); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data lembur")
			return
		}
		if branchName.Valid {
			item.BranchName = &branchName.String
		}
		if decisionNote.Valid {
			item.DecisionNote = &decisionNote.String
		}
		if dateVal.Valid {
			item.Date = dateVal.Time.Format("2006-01-02")
		}
		if decidedAt.Valid {
			s := decidedAt.Time.Format(time.RFC3339)
			item.DecidedAt = &s
		}
		if createdAt.Valid {
			item.CreatedAt = createdAt.Time.Format(time.RFC3339)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data lembur")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// Create — POST /api/hr/overtime (admin/manager) — creates a pending request.
func (h *OvertimeHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body overtimeRequestBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	empID, err := parseUUID(body.EmployeeID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "employee_id tidak valid")
		return
	}
	if body.Hours <= 0 {
		respondError(w, http.StatusBadRequest, "jam lembur harus lebih dari 0")
		return
	}
	dateVal, err := time.Parse("2006-01-02", strings.TrimSpace(body.Date))
	if err != nil {
		respondError(w, http.StatusBadRequest, "format tanggal tidak valid (YYYY-MM-DD)")
		return
	}

	ctx := r.Context()
	createdBy := middleware.UserIDFromCtx(ctx)

	reason := pgtype.Text{}
	if s := strings.TrimSpace(body.Reason); s != "" {
		reason = pgtype.Text{String: s, Valid: true}
	}

	req, err := h.queries.CreateOvertimeRequest(ctx, &db.CreateOvertimeRequestParams{
		EmployeeID: pgtype.UUID{Bytes: empID, Valid: true},
		Date:       pgtype.Date{Time: dateVal, Valid: true},
		Hours:      service.NumericFromFloat(body.Hours),
		Reason:     reason,
		CreatedBy:  pgtype.UUID{Bytes: createdBy, Valid: createdBy != [16]byte{}},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan permintaan lembur")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      createdBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "overtime_request",
		EntityID:    req.ID.Bytes,
		Description: "Menambahkan permintaan lembur karyawan",
	})

	respondJSON(w, http.StatusCreated, req)
}

// Delete — DELETE /api/hr/overtime/:id (admin only)
func (h *OvertimeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if middleware.RoleFromCtx(r.Context()) != "admin" {
		respondError(w, http.StatusForbidden, "hanya admin yang dapat menghapus permintaan lembur")
		return
	}

	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	if _, err := h.queries.GetOvertimeRequestByID(ctx, pgID); err != nil {
		if err == pgx.ErrNoRows {
			respondError(w, http.StatusNotFound, "permintaan lembur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil permintaan lembur")
		return
	}

	if err := h.queries.DeleteOvertimeRequest(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus permintaan lembur")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "overtime_request",
		EntityID:    id,
		Description: "Menghapus permintaan lembur karyawan",
	})

	w.WriteHeader(http.StatusNoContent)
}

// ── Approval workflow (mirrors leave) ────────────────────────────────────────

type overtimeDecisionBody struct {
	Note string `json:"note"`
}

// decide is the shared body for approve/reject/cancel. The query only touches a row
// in the required source state, so pgx.ErrNoRows means "wrong status" → 400.
//
// fn wraps the specific sqlc call (ApproveOvertimeRequest / RejectOvertimeRequest /
// CancelOvertimeRequest): sqlc generates a distinct Params/Row struct per query even
// though the three are structurally identical, so there is no shared db.* type to
// reference here — fn adapts each query's own types to this common shape instead.
func (h *OvertimeHandler) decide(
	w http.ResponseWriter, r *http.Request,
	fn func(ctx context.Context, id, decidedBy pgtype.UUID, note pgtype.Text) (row any, rowID pgtype.UUID, err error),
	logDesc, badStateMsg string,
) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body overtimeDecisionBody
	_ = parseBody(r, &body) // note is optional
	note := strings.TrimSpace(body.Note)

	ctx := r.Context()
	deciderID := middleware.UserIDFromCtx(ctx)

	req, reqID, err := fn(ctx,
		pgtype.UUID{Bytes: id, Valid: true},
		pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}},
		pgtype.Text{String: note, Valid: note != ""},
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			respondError(w, http.StatusBadRequest, badStateMsg)
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memproses permintaan lembur")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "overtime_request",
		EntityID:    reqID.Bytes,
		Description: logDesc,
	})

	respondJSON(w, http.StatusOK, req)
}

// Approve — POST /api/hr/overtime/{id}/approve (manager only)
func (h *OvertimeHandler) Approve(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, func(ctx context.Context, id, decidedBy pgtype.UUID, note pgtype.Text) (any, pgtype.UUID, error) {
		row, err := h.queries.ApproveOvertimeRequest(ctx, &db.ApproveOvertimeRequestParams{ID: id, DecidedBy: decidedBy, DecisionNote: note})
		if err != nil {
			return nil, pgtype.UUID{}, err
		}
		return row, row.ID, nil
	}, "Menyetujui permintaan lembur karyawan",
		"hanya pengajuan berstatus menunggu yang dapat disetujui")
}

// Reject — POST /api/hr/overtime/{id}/reject (manager only)
func (h *OvertimeHandler) Reject(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, func(ctx context.Context, id, decidedBy pgtype.UUID, note pgtype.Text) (any, pgtype.UUID, error) {
		row, err := h.queries.RejectOvertimeRequest(ctx, &db.RejectOvertimeRequestParams{ID: id, DecidedBy: decidedBy, DecisionNote: note})
		if err != nil {
			return nil, pgtype.UUID{}, err
		}
		return row, row.ID, nil
	}, "Menolak permintaan lembur karyawan",
		"hanya pengajuan berstatus menunggu yang dapat ditolak")
}

// Cancel — POST /api/hr/overtime/{id}/cancel (admin/manager)
func (h *OvertimeHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, func(ctx context.Context, id, decidedBy pgtype.UUID, note pgtype.Text) (any, pgtype.UUID, error) {
		row, err := h.queries.CancelOvertimeRequest(ctx, &db.CancelOvertimeRequestParams{ID: id, DecidedBy: decidedBy, DecisionNote: note})
		if err != nil {
			return nil, pgtype.UUID{}, err
		}
		return row, row.ID, nil
	}, "Membatalkan permintaan lembur karyawan",
		"hanya pengajuan menunggu atau disetujui yang dapat dibatalkan")
}

