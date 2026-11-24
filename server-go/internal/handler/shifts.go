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

// Roster date rule (enforced here, not in the UI): past days are read-only, today
// needs a manager (others file a change request), future days apply directly.
type ShiftHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewShiftHandler(pool *pgxpool.Pool, queries *db.Queries) *ShiftHandler {
	return &ShiftHandler{pool: pool, queries: queries}
}

// errShiftRule is a validation failure worth showing to the user verbatim.
type errShiftRule struct {
	status int
	msg    string
}

func (e *errShiftRule) Error() string { return e.msg }

func shiftRule(status int, format string, args ...any) error {
	return &errShiftRule{status: status, msg: fmt.Sprintf(format, args...)}
}

func respondShiftErr(w http.ResponseWriter, err error, fallback string) {
	var rule *errShiftRule
	if errors.As(err, &rule) {
		respondError(w, rule.status, rule.msg)
		return
	}
	respondError(w, http.StatusInternalServerError, fallback)
}

func fmtIndoDate(t time.Time) string { return t.Format("02/01/2006") }

const shiftDefinitionSelect = `
SELECT sd.id::text, sd.branch_id::text, b.name, sd.code, sd.name,
       to_char(sd.start1, 'HH24:MI'), to_char(sd.end1, 'HH24:MI'),
       to_char(sd.start2, 'HH24:MI'), to_char(sd.end2, 'HH24:MI'),
       sd.break_minutes, sd.delivery_only, sd.color, sd.sort_order, sd.is_active,
       (EXTRACT(EPOCH FROM sd.start1) / 60)::int, (EXTRACT(EPOCH FROM sd.end1) / 60)::int,
       (EXTRACT(EPOCH FROM sd.start2) / 60)::int, (EXTRACT(EPOCH FROM sd.end2) / 60)::int
FROM shift_definitions sd
JOIN branches b ON b.id = sd.branch_id`

type shiftProfileBody struct {
	DivisionID      *string `json:"division_id"`
	IsDeliveryStaff bool    `json:"is_delivery_staff"`
}

// UpdateEmployeeShiftProfile — PUT /api/hr/employees/{id}/shift-profile
// Un-flagging DO staff still rostered on a delivery-only shift is refused.
func (h *ShiftHandler) UpdateEmployeeShiftProfile(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body shiftProfileBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	divisionID, err := optUUID(body.DivisionID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "divisi tidak valid")
		return
	}
	ctx := r.Context()
	empID := pgUUID(id)

	var name string
	var branchID pgtype.UUID
	if err := h.pool.QueryRow(ctx, `SELECT full_name, branch_id FROM employees WHERE id = $1`, empID).Scan(&name, &branchID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil karyawan")
		return
	}
	var divName string
	if divisionID.Valid {
		var divBranch pgtype.UUID
		if err := h.pool.QueryRow(ctx, `SELECT branch_id, name FROM divisions WHERE id = $1`, divisionID).Scan(&divBranch, &divName); err != nil {
			respondError(w, http.StatusBadRequest, "divisi tidak ditemukan")
			return
		}
		if divBranch != branchID {
			respondError(w, http.StatusBadRequest, "divisi bukan milik cabang karyawan ini")
			return
		}
	}
	if !body.IsDeliveryStaff {
		var n int
		if err := h.pool.QueryRow(ctx, `
			SELECT count(*) FROM shift_assignments sa JOIN shift_definitions sd ON sd.id = sa.shift_id
			WHERE sa.employee_id = $1 AND sa.date >= $2 AND sd.delivery_only`,
			empID, pgtype.Date{Time: today(), Valid: true}).Scan(&n); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memeriksa jadwal")
			return
		}
		if n > 0 {
			respondError(w, http.StatusConflict, fmt.Sprintf(
				"%s masih dijadwalkan %d hari di shift khusus delivery order — ubah jadwalnya dulu", name, n))
			return
		}
	}
	if _, err := h.pool.Exec(ctx, `UPDATE employees SET division_id = $2, is_delivery_staff = $3, updated_at = now() WHERE id = $1`,
		empID, divisionID, body.IsDeliveryStaff); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan profil shift")
		return
	}
	if divName == "" {
		divName = "tanpa divisi"
	}
	do := "bukan staf DO"
	if body.IsDeliveryStaff {
		do = "staf DO"
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "employee",
		EntityID:    id,
		Description: fmt.Sprintf("Mengubah profil shift %s: divisi %s, %s", name, divName, do),
	})
	respondJSON(w, http.StatusOK, map[string]any{"division_id": body.DivisionID, "is_delivery_staff": body.IsDeliveryStaff})
}
