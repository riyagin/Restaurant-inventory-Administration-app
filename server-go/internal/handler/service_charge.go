package handler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Service Charge pool screen (migration 091, service/service_charge.go).
//
//	GET    /api/hr/service-charge?month=YYYY-MM   every branch: revenue, pool, shares
//	PUT    /api/hr/service-charge                 save one branch's pool for a month
//	DELETE /api/hr/service-charge?branch_id=&month=
//	PUT    /api/hr/service-charge/component       which wage component is Service Charge
type ServiceChargeHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewServiceChargeHandler(pool *pgxpool.Pool, queries *db.Queries) *ServiceChargeHandler {
	return &ServiceChargeHandler{pool: pool, queries: queries}
}

// branchRevenueForMonth is each branch's net revenue for a calendar month. It
// composes the shared P&L definitions — plActivityByAccount for what an
// account did and accountBranchOwnerSQL for who owns it — rather than growing
// a third spelling of either. POS revenue is already posted net of discount
// (handler/pos_import.go), so the revenue accounts alone are net revenue.
func branchRevenueForMonth(ctx context.Context, pool *pgxpool.Pool, month time.Time) (map[[16]byte]int64, error) {
	start := month.Format("2006-01-02")
	end := month.AddDate(0, 1, -1).Format("2006-01-02")
	activity, err := plActivityByAccount(ctx, pool, start, end)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `WITH RECURSIVE `+accountBranchOwnerCTE+`,
own AS (SELECT DISTINCT ON (account_id) account_id, branch_id FROM owned ORDER BY account_id, depth)
SELECT own.account_id, own.branch_id FROM own
JOIN accounts a ON a.id = own.account_id
WHERE a.account_type = 'revenue'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[[16]byte]int64{}
	for rows.Next() {
		var accountID, branchID pgtype.UUID
		if err := rows.Scan(&accountID, &branchID); err != nil {
			return nil, err
		}
		out[branchID.Bytes] += activity[uuidBytesToString(accountID.Bytes)]
	}
	return out, rows.Err()
}

type scPoolJSON struct {
	InputMode     string      `json:"input_mode"`
	Percent       *float64    `json:"percent"`
	PoolAmount    int64       `json:"pool_amount"`
	RevenueAmount int64       `json:"revenue_amount"` // frozen when saved
	Note          *string     `json:"note"`
	UpdatedAt     time.Time   `json:"updated_at"`
	UpdatedBy     pgtype.UUID `json:"updated_by"`
}

func poolJSON(p *db.ServiceChargePool) *scPoolJSON {
	if p == nil {
		return nil
	}
	out := &scPoolJSON{InputMode: p.InputMode, PoolAmount: p.PoolAmount, RevenueAmount: p.RevenueAmount,
		UpdatedAt: p.UpdatedAt.Time, UpdatedBy: p.UpdatedBy}
	if p.PercentBp.Valid {
		v := float64(p.PercentBp.Int32) / 100
		out.Percent = &v
	}
	if p.Note.Valid {
		out.Note = &p.Note.String
	}
	return out
}

type scBranchJSON struct {
	BranchID   string      `json:"branch_id"`
	BranchName string      `json:"branch_name"`
	Revenue    int64       `json:"revenue"` // live
	Pool       *scPoolJSON `json:"pool"`
	*service.ServiceChargeDistribution
}

// scPeriodState describes the month's payroll period, if any. Once lines are
// generated they hold a frozen Service Charge; a closed period can no longer
// be regenerated, so its pools are locked.
type scPeriodState struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	LineCount int64  `json:"line_count"`
}

func (h *ServiceChargeHandler) periodState(ctx context.Context, month time.Time) (*scPeriodState, error) {
	p, err := h.queries.GetPayrollPeriodByMonth(ctx, pgtype.Date{Time: month, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st := &scPeriodState{ID: uuidBytesToString(p.ID.Bytes), Status: p.Status}
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM payroll_lines WHERE payroll_period_id = $1`, p.ID).Scan(&st.LineCount); err != nil {
		return nil, err
	}
	return st, nil
}

// Get — GET /api/hr/service-charge?month=YYYY-MM[&branch_id=]
func (h *ServiceChargeHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	month, err := firstOfMonthDate(r.URL.Query().Get("month"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
		return
	}
	onlyBranch := strings.TrimSpace(r.URL.Query().Get("branch_id"))

	revenue, err := branchRevenueForMonth(ctx, h.pool, month)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung pendapatan cabang")
		return
	}
	asOf, err := service.ServiceChargeAsOf(ctx, h.queries, month)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menentukan periode gaji")
		return
	}
	in, err := service.LoadServiceChargeInput(ctx, h.queries, month, asOf)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memuat data service charge")
		return
	}

	// Component picker: the configured one plus the candidates.
	type compJSON struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		GradeLinked bool   `json:"grade_linked"`
	}
	var component *compJSON
	candidates := []compJSON{}
	comps, err := h.queries.ListWageComponents(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil komponen upah")
		return
	}
	for _, c := range comps {
		cj := compJSON{ID: uuidBytesToString(c.ID.Bytes), Name: c.Name, Type: c.Type, GradeLinked: c.GradeLinked}
		if in.ComponentID.Valid && c.ID == in.ComponentID {
			component = &cj
		}
		if c.IsActive && (c.Type == service.ComponentTypeBonus || c.Type == service.ComponentTypeAllowance) {
			candidates = append(candidates, cj)
		}
	}

	period, err := h.periodState(ctx, month)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil periode gaji")
		return
	}

	bRows, err := h.pool.Query(ctx, `SELECT id, name FROM branches ORDER BY name`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data cabang")
		return
	}
	type br struct {
		id   pgtype.UUID
		name string
	}
	branches, err := pgx.CollectRows(bRows, func(row pgx.CollectableRow) (br, error) {
		var b br
		return b, row.Scan(&b.id, &b.name)
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data cabang")
		return
	}

	out := []scBranchJSON{}
	var totalRevenue, totalPool, totalDistributed int64
	for _, b := range branches {
		id := uuidBytesToString(b.id.Bytes)
		if onlyBranch != "" && onlyBranch != id {
			continue
		}
		d, err := service.ServiceChargeForBranch(ctx, h.queries, in, b.id)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghitung pembagian service charge")
			return
		}
		out = append(out, scBranchJSON{BranchID: id, BranchName: b.name, Revenue: revenue[b.id.Bytes],
			Pool: poolJSON(d.Pool), ServiceChargeDistribution: d})
		totalRevenue += revenue[b.id.Bytes]
		totalPool += d.PoolAmount
		totalDistributed += d.Distributed
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"month":             month.Format("2006-01"),
		"as_of":             asOf.Format("2006-01-02"),
		"component":         component,
		"candidates":        candidates,
		"payroll_period":    period,
		"branches":          out,
		"total_revenue":     totalRevenue,
		"total_pool":        totalPool,
		"total_distributed": totalDistributed,
	})
}

// lockedPeriodError refuses edits once the month's payroll is closed: its lines
// can no longer be regenerated, so a new pot could never be paid.
func (h *ServiceChargeHandler) lockedPeriodError(ctx context.Context, month time.Time) (int, string) {
	st, err := h.periodState(ctx, month)
	if err != nil {
		return http.StatusInternalServerError, "gagal mengambil periode gaji"
	}
	if st != nil && st.Status != "open" {
		return http.StatusConflict, "periode gaji bulan ini sudah ditutup — service charge tidak dapat diubah"
	}
	return 0, ""
}

// Save — PUT /api/hr/service-charge
// Body: {branch_id, month: "YYYY-MM", input_mode: "amount"|"percent", amount?, percent?, note?}
func (h *ServiceChargeHandler) Save(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		BranchID  string   `json:"branch_id"`
		Month     string   `json:"month"`
		InputMode string   `json:"input_mode"`
		Amount    *int64   `json:"amount"`
		Percent   *float64 `json:"percent"`
		Note      string   `json:"note"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "data tidak valid")
		return
	}
	branchID, err := parseUUID(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "cabang tidak valid")
		return
	}
	if strings.TrimSpace(body.Month) == "" {
		respondError(w, http.StatusBadRequest, "bulan wajib diisi")
		return
	}
	month, err := firstOfMonthDate(body.Month)
	if err != nil {
		respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
		return
	}
	if code, msg := h.lockedPeriodError(ctx, month); code != 0 {
		respondError(w, code, msg)
		return
	}

	revenueByBranch, err := branchRevenueForMonth(ctx, h.pool, month)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung pendapatan cabang")
		return
	}
	revenue := revenueByBranch[branchID]

	params := &db.UpsertServiceChargePoolParams{
		BranchID:      pgtype.UUID{Bytes: branchID, Valid: true},
		PeriodMonth:   pgtype.Date{Time: month, Valid: true},
		RevenueAmount: revenue,
		InputMode:     body.InputMode,
		UpdatedBy:     pgtype.UUID{Bytes: middleware.UserIDFromCtx(ctx), Valid: middleware.UserIDFromCtx(ctx) != uuid.Nil},
	}
	if n := strings.TrimSpace(body.Note); n != "" {
		params.Note = pgtype.Text{String: n, Valid: true}
	}
	var desc string
	switch body.InputMode {
	case "amount":
		if body.Amount == nil || *body.Amount < 0 {
			respondError(w, http.StatusBadRequest, "nominal service charge wajib diisi dan tidak boleh negatif")
			return
		}
		params.PoolAmount = *body.Amount
		desc = fmt.Sprintf("nominal Rp%d", params.PoolAmount)
	case "percent":
		if body.Percent == nil || *body.Percent < 0 || *body.Percent > 100 || math.IsNaN(*body.Percent) {
			respondError(w, http.StatusBadRequest, "persentase harus 0–100")
			return
		}
		bp := int32(math.Round(*body.Percent * 100))
		params.PercentBp = pgtype.Int4{Int32: bp, Valid: true}
		if revenue > 0 {
			params.PoolAmount = (revenue*int64(bp) + 5000) / 10000
		}
		desc = fmt.Sprintf("%.2f%% dari pendapatan Rp%d = Rp%d", float64(bp)/100, revenue, params.PoolAmount)
	default:
		respondError(w, http.StatusBadRequest, "input_mode harus 'amount' atau 'percent'")
		return
	}

	pool, err := h.queries.UpsertServiceChargePool(ctx, params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan service charge")
		return
	}
	logMutation(r, h.queries, "UPDATE", "service_charge", uuidFromPg(pool.ID),
		fmt.Sprintf("Service charge %s cabang %s: %s", month.Format("2006-01"), body.BranchID, desc))
	respondJSON(w, http.StatusOK, poolJSON(pool))
}

// Delete — DELETE /api/hr/service-charge?branch_id=&month=
func (h *ServiceChargeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	branchID, err := parseUUID(r.URL.Query().Get("branch_id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "cabang tidak valid")
		return
	}
	month, err := firstOfMonthDate(r.URL.Query().Get("month"))
	if err != nil || strings.TrimSpace(r.URL.Query().Get("month")) == "" {
		respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
		return
	}
	if code, msg := h.lockedPeriodError(ctx, month); code != 0 {
		respondError(w, code, msg)
		return
	}
	if err := h.queries.DeleteServiceChargePool(ctx, &db.DeleteServiceChargePoolParams{
		BranchID: pgtype.UUID{Bytes: branchID, Valid: true}, PeriodMonth: pgtype.Date{Time: month, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus service charge")
		return
	}
	logMutation(r, h.queries, "DELETE", "service_charge", branchID,
		fmt.Sprintf("Service charge %s cabang %s dihapus", month.Format("2006-01"), branchID))
	w.WriteHeader(http.StatusNoContent)
}

// SetComponent — PUT /api/hr/service-charge/component  {wage_component_id: uuid|null}
func (h *ServiceChargeHandler) SetComponent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WageComponentID *string `json:"wage_component_id"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "data tidak valid")
		return
	}
	var id pgtype.UUID
	if body.WageComponentID != nil && *body.WageComponentID != "" {
		u, err := parseUUID(*body.WageComponentID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "komponen tidak valid")
			return
		}
		id = pgtype.UUID{Bytes: u, Valid: true}
	}
	if err := h.queries.SetServiceChargeComponentID(r.Context(), id); err != nil {
		respondError(w, http.StatusBadRequest, "gagal menyimpan komponen service charge")
		return
	}
	logMutation(r, h.queries, "UPDATE", "service_charge", uuid.UUID(id.Bytes), "Komponen service charge diubah")
	respondJSON(w, http.StatusOK, map[string]any{"wage_component_id": id})
}
