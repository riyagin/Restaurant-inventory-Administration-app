package service

import "time"

// employees.employment_type values. EmploymentTypeContract lives in thr.go.
const (
	EmploymentTypePermanent = "permanent" // tetap (PKWTT)
	EmploymentTypeProbation = "probation" // masa percobaan
)

// ProbationMonths is how long a probation lasts, counted from the join date.
const ProbationMonths = 1

// ProbationEndDate returns the last day of probation for an employee who joined
// on joinDate: the same day-of-month ProbationMonths later, clamped to that
// month's last day (31 Jan → 28/29 Feb, not 2/3 Mar as time.AddDate would give).
func ProbationEndDate(joinDate time.Time) time.Time {
	y, m, d := joinDate.Date()
	target := time.Date(y, m+ProbationMonths, 1, 0, 0, 0, 0, joinDate.Location())
	last := target.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(target.Year(), target.Month(), d, 0, 0, 0, 0, joinDate.Location())
}
