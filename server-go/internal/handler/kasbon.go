package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

type KasbonHandler struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	uploadsDir string
}

func NewKasbonHandler(pool *pgxpool.Pool, queries *db.Queries) *KasbonHandler {
	return &KasbonHandler{pool: pool, queries: queries}
}

// SetUploadsDir injects the uploads directory into the handler.
func (h *KasbonHandler) SetUploadsDir(dir string) {
	h.uploadsDir = dir
}

func (h *KasbonHandler) resolveUploadsDir() string {
	if h.uploadsDir != "" {
		return h.uploadsDir
	}
	return config.DefaultUploadsDir
}

// parseMonth parses a "YYYY-MM" or "YYYY-MM-DD" string into a first-of-month UTC date.
func parseMonth(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if len(s) == 7 {
		s += "-01"
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	return service.FirstOfMonth(t), nil
}

// kasbonInstallmentInput is the request payload for one installment row.
type kasbonInstallmentInput struct {
	DueMonth string `json:"due_month"`
	Amount   int64  `json:"amount"`
}

type kasbonBody struct {
	EmployeeID          string                   `json:"employee_id"`
	Amount              int64                    `json:"amount"`
	Details             string                   `json:"details"`
	SendingMethod       string                   `json:"sending_method"`
	FundSourceAccountID string                   `json:"fund_source_account_id"`
	ResolutionMonth     string                   `json:"resolution_month"`
	Installments        []kasbonInstallmentInput `json:"installments"`
}

// kasbonView is a kasbon plus its installments and (optionally) the employee's last
// resolved kasbon info.
type kasbonView struct {
	*db.Kasbon
	Installments []*db.KasbonInstallment      `json:"installments"`
	LastResolved *db.GetLastResolvedKasbonRow `json:"last_resolved,omitempty"`
	// Kebijakan Kasbon (migration 088): the score the request was judged on
	// and its track — priority (≥ 90) or evaluation (70–89).
	ScoreAtRequest pgtype.Int4 `json:"score_at_request"`
	ApprovalTrack  pgtype.Text `json:"approval_track"`
}

// kasbonListRow adds the policy check to a listed kasbon.
type kasbonListRow struct {
	*db.ListKasbonsRow
	ScoreAtRequest pgtype.Int4 `json:"score_at_request"`
	ApprovalTrack  pgtype.Text `json:"approval_track"`
}

// List — GET /api/hr/kasbons?status=&employee_id=&q=
func (h *KasbonHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	params := &db.ListKasbonsParams{
		Status: strings.TrimSpace(q.Get("status")),
		Q:      strings.TrimSpace(q.Get("q")),
	}
	if v := strings.TrimSpace(q.Get("employee_id")); v != "" {
		eid, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "employee_id tidak valid")
			return
		}
		params.EmployeeID = pgtype.UUID{Bytes: eid, Valid: true}
	}
	if b, ok := scopedBranch(r); ok {
		params.BranchID = b
	}

	rows, err := h.queries.ListKasbons(ctx, params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kasbon")
		return
	}
	checks, err := h.queries.ListKasbonPolicyChecks(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kasbon")
		return
	}
	byID := make(map[[16]byte]*db.ListKasbonPolicyChecksRow, len(checks))
	for _, c := range checks {
		byID[c.ID.Bytes] = c
	}
	out := make([]kasbonListRow, 0, len(rows))
	for _, row := range rows {
		v := kasbonListRow{ListKasbonsRow: row}
		if c := byID[row.ID.Bytes]; c != nil {
			v.ScoreAtRequest, v.ApprovalTrack = c.ScoreAtRequest, c.ApprovalTrack
		}
		out = append(out, v)
	}
	respondJSON(w, http.StatusOK, out)
}

// Get — GET /api/hr/kasbons/:id (incl. installments + last resolved info)
func (h *KasbonHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	k, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, k.EmployeeID) {
		return
	}

	installments, err := h.queries.ListKasbonInstallments(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil cicilan kasbon")
		return
	}
	if installments == nil {
		installments = []*db.KasbonInstallment{}
	}

	lastResolved, _ := service.LastResolvedKasbon(ctx, h.queries, k.EmployeeID)

	view := kasbonView{Kasbon: k, Installments: installments, LastResolved: lastResolved}
	if c, err := h.queries.GetKasbonPolicyCheck(ctx, pgID); err == nil && c != nil {
		view.ScoreAtRequest, view.ApprovalTrack = c.ScoreAtRequest, c.ApprovalTrack
	}
	respondJSON(w, http.StatusOK, view)
}
