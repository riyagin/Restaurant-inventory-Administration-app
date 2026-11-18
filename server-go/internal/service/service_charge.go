package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Service Charge pool (migration 091).
//
// HR sets, per branch and calendar month, how much is shared out as Service
// Charge (a typed amount, or a percentage of the branch's net revenue). The pot
// is split between the branch's members — active employees whose wage structure
// carries the Service Charge component — one share each, scaled by the
// percentage their grade pays for that component (A 100% → 1 share, B 50% → ½,
// 0% → none; a missing score pays in full, as everywhere else in payroll).
//
//	amount_i = pool × weight_i / Σ weights
//
// so a share one employee forfeits is spread over the others, and the pot is
// paid out to the rupiah (largest remainder) as long as anyone is eligible.
//
// The score month is the pool's month, as for every grade-linked component.
// Payroll (GenerateLines and the single-line regenerate) uses the share as the
// component's amount in place of fixed amount × grade; a branch without a pool
// for the month falls back to that old rule. The evaluation screen and payroll
// both go through ServiceChargeForBranch, so they cannot disagree.

// Reasons a member's weight is zero (shown on the screen).
const (
	ServiceChargeExcludedGrade = "grade" // their tier pays 0% of Service Charge
)

// ServiceChargeShare is one member's slice of a branch's pot.
type ServiceChargeShare struct {
	EmployeeID   pgtype.UUID `json:"employee_id"`
	EmployeeCode string      `json:"employee_code"`
	FullName     string      `json:"full_name"`
	Score        *int32      `json:"score"` // nil: not evaluated yet (pays in full)
	Grade        string      `json:"grade"`
	Weight       int32       `json:"weight"` // the grade's Service Charge %, 0–100
	Amount       int64       `json:"amount"`
	Excluded     string      `json:"excluded,omitempty"`
}

// ServiceChargeDistribution is a branch's pot and how it splits.
type ServiceChargeDistribution struct {
	Pool          *db.ServiceChargePool `json:"-"`
	PoolAmount    int64                 `json:"pool_amount"`
	TotalWeight   int64                 `json:"total_weight"`
	Distributed   int64                 `json:"distributed"`
	MemberCount   int                   `json:"member_count"`
	EligibleCount int                   `json:"eligible_count"`
	Shares        []ServiceChargeShare  `json:"shares"`
}

// SplitByWeight divides total over weights in proportion, rounding down and
// handing the leftover rupiah one by one to the largest remainders (ties to the
// earlier index), so the parts always sum to total. All-zero weights → all 0.
func SplitByWeight(total int64, weights []int64) []int64 {
	out := make([]int64, len(weights))
	var sum int64
	for _, w := range weights {
		if w > 0 {
			sum += w
		}
	}
	if sum == 0 || total <= 0 {
		return out
	}
	type rem struct {
		i int
		r int64
	}
	rems := make([]rem, 0, len(weights))
	var given int64
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		out[i] = total * w / sum
		given += out[i]
		rems = append(rems, rem{i, total * w % sum})
	}
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].r > rems[b].r })
	for k := 0; given < total; k++ {
		out[rems[k%len(rems)].i]++
		given++
	}
	return out
}

// LoadServiceChargeComponentID returns the wage component configured as
// Service Charge, or an invalid UUID when none is set.
func LoadServiceChargeComponentID(ctx context.Context, q *db.Queries) (pgtype.UUID, error) {
	id, err := q.GetServiceChargeComponentID(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, nil
	}
	return id, err
}

// ServiceChargeAsOf is the date wage structures are read at for a month's
// pool: the payroll period's end date (what GenerateLines uses), or, before the
// period exists, the end it would get.
func ServiceChargeAsOf(ctx context.Context, q *db.Queries, month time.Time) (time.Time, error) {
	p, err := q.GetPayrollPeriodByMonth(ctx, pgtype.Date{Time: FirstOfMonth(month), Valid: true})
	if err == nil && p != nil && p.EndDate.Valid {
		return p.EndDate.Time, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, err
	}
	_, end, err := ResolvePayrollPeriodBounds(ctx, q, month)
	if err != nil {
		// A period that cannot be resolved (e.g. overlapping) still has a month.
		return FirstOfMonth(month).AddDate(0, 1, -1), nil
	}
	return end, nil
}

// ServiceChargeInput is what ServiceChargeForBranch needs besides the branch;
// callers that handle many branches load it once.
type ServiceChargeInput struct {
	Month       time.Time
	AsOf        time.Time
	Grades      GradeScale
	ComponentID pgtype.UUID
	Employees   []*db.ListActiveEmployeesForPayrollRow
}

// LoadServiceChargeInput loads the shared inputs for a month.
func LoadServiceChargeInput(ctx context.Context, q *db.Queries, month, asOf time.Time) (*ServiceChargeInput, error) {
	cid, err := LoadServiceChargeComponentID(ctx, q)
	if err != nil {
		return nil, err
	}
	grades, err := LoadGradeScale(ctx, q)
	if err != nil {
		return nil, err
	}
	emps, err := q.ListActiveEmployeesForPayroll(ctx)
	if err != nil {
		return nil, err
	}
	return &ServiceChargeInput{Month: FirstOfMonth(month), AsOf: asOf, Grades: grades, ComponentID: cid, Employees: emps}, nil
}

// ServiceChargeForBranch computes a branch's members and their shares. With no
// pool saved, Pool is nil and every amount is 0 (members are still listed).
func ServiceChargeForBranch(ctx context.Context, q *db.Queries, in *ServiceChargeInput, branchID pgtype.UUID) (*ServiceChargeDistribution, error) {
	d := &ServiceChargeDistribution{Shares: []ServiceChargeShare{}}
	monthDate := pgtype.Date{Time: in.Month, Valid: true}
	pool, err := q.GetServiceChargePool(ctx, &db.GetServiceChargePoolParams{BranchID: branchID, PeriodMonth: monthDate})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		d.Pool = pool
		d.PoolAmount = pool.PoolAmount
	}
	if !in.ComponentID.Valid {
		return d, nil
	}

	var weights []int64
	for _, emp := range in.Employees {
		if emp.BranchID != branchID {
			continue
		}
		ws, err := GetCurrentWage(ctx, q, emp.ID, in.AsOf)
		if err != nil {
			return nil, err
		}
		if ws == nil {
			continue
		}
		comps, err := q.ListEmployeeWageComponents(ctx, ws.ID)
		if err != nil {
			return nil, err
		}
		var sc *db.ListEmployeeWageComponentsRow
		for _, c := range comps {
			if c.WageComponentID == in.ComponentID {
				sc = c
				break
			}
		}
		if sc == nil {
			continue // not a Service Charge member
		}

		var score pgtype.Int4
		if s, serr := q.GetPerformanceScore(ctx, &db.GetPerformanceScoreParams{EmployeeID: emp.ID, PeriodMonth: monthDate}); serr == nil && s != nil {
			score = pgtype.Int4{Int32: s.Score, Valid: true}
		} else if serr != nil && !errors.Is(serr, pgx.ErrNoRows) {
			return nil, serr
		}

		share := ServiceChargeShare{EmployeeID: emp.ID, EmployeeCode: emp.EmployeeCode, FullName: emp.FullName, Weight: 100}
		if score.Valid {
			v := score.Int32
			share.Score = &v
		}
		if tier := in.Grades.Resolve(score); tier != nil {
			share.Grade = tier.Label
			if sc.ComponentGradeLinked {
				share.Weight = tier.PercentFor(in.ComponentID)
			}
		}
		if share.Weight <= 0 {
			share.Weight = 0
			share.Excluded = ServiceChargeExcludedGrade
		} else {
			d.EligibleCount++
		}
		d.TotalWeight += int64(share.Weight)
		d.Shares = append(d.Shares, share)
		weights = append(weights, int64(share.Weight))
	}
	d.MemberCount = len(d.Shares)

	for i, a := range SplitByWeight(d.PoolAmount, weights) {
		d.Shares[i].Amount = a
		d.Distributed += a
	}
	return d, nil
}

// ServiceChargeResolver answers "what is this employee's Service Charge this
// period" for payroll, computing each branch's split once.
type ServiceChargeResolver struct {
	q      *db.Queries
	in     *ServiceChargeInput
	byBran map[[16]byte]map[[16]byte]int64 // nil inner map: branch has no pool
}

// NewServiceChargeResolver prepares a resolver for a payroll period.
func NewServiceChargeResolver(ctx context.Context, q *db.Queries, period *db.PayrollPeriod) (*ServiceChargeResolver, error) {
	in, err := LoadServiceChargeInput(ctx, q, period.PeriodMonth.Time, period.EndDate.Time)
	if err != nil {
		return nil, err
	}
	return &ServiceChargeResolver{q: q, in: in, byBran: map[[16]byte]map[[16]byte]int64{}}, nil
}

// Amount returns the employee's pool share for component componentID. ok is
// false when componentID is not the Service Charge component or the branch has
// no pool for the month — the caller then applies the normal component rule.
func (r *ServiceChargeResolver) Amount(ctx context.Context, branchID, employeeID, componentID pgtype.UUID) (int64, bool, error) {
	if r == nil || !r.in.ComponentID.Valid || componentID != r.in.ComponentID {
		return 0, false, nil
	}
	shares, seen := r.byBran[branchID.Bytes]
	if !seen {
		d, err := ServiceChargeForBranch(ctx, r.q, r.in, branchID)
		if err != nil {
			return 0, false, err
		}
		if d.Pool != nil {
			shares = make(map[[16]byte]int64, len(d.Shares))
			for _, s := range d.Shares {
				shares[s.EmployeeID.Bytes] = s.Amount
			}
		}
		r.byBran[branchID.Bytes] = shares
	}
	if shares == nil {
		return 0, false, nil
	}
	return shares[employeeID.Bytes], true, nil
}
