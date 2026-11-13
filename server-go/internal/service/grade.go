package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Graded benefits (migration 087).
//
// One company-wide grade scale maps the monthly performance score (100 −
// violation points) to the share of every grade-linked wage component that is
// paid: e.g. A ≥ 90 → 100%, B ≥ 80 → 75%, C ≥ 70 → 50%, D ≥ 0 → 0%. The tier
// used is the one with the highest min_score the score reaches — exactly one.
//
// It replaces the old per-component min_score gate, which paid all or nothing.
// Components that are not grade-linked are unaffected, and a missing score
// (evaluation not run yet) pays in full, as the gate did: pay is never withheld
// silently on account of a missing evaluation.

// GradeTier is one row of the scale.
type GradeTier struct {
	Label      string `json:"label"`
	MinScore   int32  `json:"min_score"`
	PayPercent int32  `json:"pay_percent"`
}

// GradeScale is the scale ordered by MinScore, highest first.
type GradeScale []GradeTier

// LoadGradeScale reads the configured scale (ordered highest first).
func LoadGradeScale(ctx context.Context, q *db.Queries) (GradeScale, error) {
	rows, err := q.ListPerformanceGrades(ctx)
	if err != nil {
		return nil, err
	}
	s := make(GradeScale, 0, len(rows))
	for _, r := range rows {
		s = append(s, GradeTier{Label: r.Label, MinScore: r.MinScore, PayPercent: r.PayPercent})
	}
	sort.SliceStable(s, func(i, j int) bool { return s[i].MinScore > s[j].MinScore })
	return s, nil
}

// Resolve returns the tier a score falls in, or nil when there is no score or
// no scale (grade-linked components then pay in full). A score below every
// tier — impossible once ValidateGradeScale has run, since it requires a 0
// tier — resolves to an unnamed 0% tier rather than paying in full.
func (s GradeScale) Resolve(score pgtype.Int4) *GradeTier {
	if !score.Valid || len(s) == 0 {
		return nil
	}
	for i := range s {
		if score.Int32 >= s[i].MinScore {
			t := s[i]
			return &t
		}
	}
	return &GradeTier{Label: "-", MinScore: 0, PayPercent: 0}
}

// ApplyGradePercent scales an amount by percent, rounding half up to the rupiah.
func ApplyGradePercent(amount int64, percent int32) int64 {
	if percent >= 100 {
		return amount
	}
	if percent <= 0 {
		return 0
	}
	return (amount*int64(percent) + 50) / 100
}

// GradedAmount is a component's payable amount after the grade: unchanged when
// the component is not grade-linked or no tier applies.
func GradedAmount(amount int64, gradeLinked bool, tier *GradeTier) int64 {
	if !gradeLinked || tier == nil {
		return amount
	}
	return ApplyGradePercent(amount, tier.PayPercent)
}

// gradeSnapshot is what a payroll line freezes about its grade.
func gradeSnapshot(tier *GradeTier) (pgtype.Text, pgtype.Int4) {
	if tier == nil {
		return pgtype.Text{}, pgtype.Int4{}
	}
	return pgtype.Text{String: tier.Label, Valid: true}, pgtype.Int4{Int32: tier.PayPercent, Valid: true}
}

// FreezePayrollLineGrade records the tier a freshly generated line was paid at.
func FreezePayrollLineGrade(ctx context.Context, qtx *db.Queries, lineID pgtype.UUID, tier *GradeTier) error {
	label, pct := gradeSnapshot(tier)
	return qtx.SetPayrollLineGrade(ctx, &db.SetPayrollLineGradeParams{
		ID: lineID, PerformanceGrade: label, GradePayPercent: pct,
	})
}

// ErrInvalidGradeScale wraps every validation failure of a submitted scale.
var ErrInvalidGradeScale = errors.New("skala grade tidak valid")

// ValidateGradeScale normalises and checks a scale submitted for saving: at
// least one tier, labels non-empty and unique, scores 0–100 and unique,
// percentages 0–100, a tier starting at 0 (so every score maps to a tier), and
// pay never rising as the score falls. Returns the tiers ordered highest first.
func ValidateGradeScale(in []GradeTier) (GradeScale, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("%w: minimal satu grade", ErrInvalidGradeScale)
	}
	s := make(GradeScale, len(in))
	labels := map[string]bool{}
	scores := map[int32]bool{}
	for i, t := range in {
		t.Label = strings.TrimSpace(t.Label)
		if t.Label == "" {
			return nil, fmt.Errorf("%w: nama grade wajib diisi", ErrInvalidGradeScale)
		}
		key := strings.ToLower(t.Label)
		if labels[key] {
			return nil, fmt.Errorf("%w: nama grade %q dipakai dua kali", ErrInvalidGradeScale, t.Label)
		}
		labels[key] = true
		if t.MinScore < 0 || t.MinScore > 100 {
			return nil, fmt.Errorf("%w: skor minimum grade %s harus 0–100", ErrInvalidGradeScale, t.Label)
		}
		if scores[t.MinScore] {
			return nil, fmt.Errorf("%w: skor minimum %d dipakai dua kali", ErrInvalidGradeScale, t.MinScore)
		}
		scores[t.MinScore] = true
		if t.PayPercent < 0 || t.PayPercent > 100 {
			return nil, fmt.Errorf("%w: persentase grade %s harus 0–100", ErrInvalidGradeScale, t.Label)
		}
		s[i] = t
	}
	if !scores[0] {
		return nil, fmt.Errorf("%w: harus ada grade dengan skor minimum 0 agar setiap skor mendapat grade", ErrInvalidGradeScale)
	}
	sort.SliceStable(s, func(i, j int) bool { return s[i].MinScore > s[j].MinScore })
	for i := 1; i < len(s); i++ {
		if s[i].PayPercent > s[i-1].PayPercent {
			return nil, fmt.Errorf("%w: grade %s (skor lebih rendah) tidak boleh dibayar lebih tinggi dari grade %s",
				ErrInvalidGradeScale, s[i].Label, s[i-1].Label)
		}
	}
	return s, nil
}

// SaveGradeScale replaces the whole scale. Must run inside a transaction.
func SaveGradeScale(ctx context.Context, qtx *db.Queries, s GradeScale) error {
	if err := qtx.DeleteAllPerformanceGrades(ctx); err != nil {
		return err
	}
	for _, t := range s {
		if _, err := qtx.CreatePerformanceGrade(ctx, &db.CreatePerformanceGradeParams{
			Label: t.Label, MinScore: t.MinScore, PayPercent: t.PayPercent,
		}); err != nil {
			return err
		}
	}
	return nil
}
