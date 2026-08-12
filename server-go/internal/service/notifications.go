package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The notification feed behind the navbar bell.
//
// One endpoint, one shape, assembled per role: everybody sees what they are
// actually responsible for and nothing they cannot act on. Every entry carries a
// link, because a notification you cannot act on from where you are is just
// anxiety.

// Severity ranks an entry. The bell badge counts `alert` and `warn` only —
// `info` is context, not a demand.
const (
	SeverityAlert = "alert" // overdue: should already have happened
	SeverityWarn  = "warn"  // due now, or waiting on this person
	SeverityInfo  = "info"
)

type Notification struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Link     string `json:"link"`
	Count    int    `json:"count,omitempty"`
}

// NotificationsFor assembles the feed for one user.
//
// Roles map to responsibilities, not to seniority: staff and admin run the daily
// desk, managers approve, the HR role watches the people queues. Admin sees the
// operational feed because it can act on all of it. superuser holds every
// capability, so it gets every section.
func NotificationsFor(ctx context.Context, pool *pgxpool.Pool, role string, today time.Time) ([]Notification, error) {
	out := []Notification{}

	super := role == "superuser"

	operational := super || role == "admin" || role == "manager" || role == "staff"
	if operational {
		tasks, err := dailyTaskNotifications(ctx, pool, today)
		if err != nil {
			return nil, err
		}
		out = append(out, tasks...)

		low, err := lowStockNotification(ctx, pool)
		if err != nil {
			return nil, err
		}
		out = append(out, low...)

		cash, err := cashVarianceNotifications(ctx, pool, today)
		if err != nil {
			return nil, err
		}
		out = append(out, cash...)
	}

	// Approval is manager-only everywhere else in the app; the feed follows.
	if super || role == "manager" {
		approvals, err := approvalNotifications(ctx, pool)
		if err != nil {
			return nil, err
		}
		out = append(out, approvals...)
	}

	if super || role == "hr" || role == "manager" || role == "admin" {
		hr, err := hrNotifications(ctx, pool, today)
		if err != nil {
			return nil, err
		}
		out = append(out, hr...)
	}

	return out, nil
}

// dailyTaskNotifications turns unfinished duties into entries. Today's are a
// reminder; anything older is an alert, and the two are never merged — "you
// still owe Tuesday" is a different message from "today isn't done yet".
func dailyTaskNotifications(ctx context.Context, pool *pgxpool.Pool, today time.Time) ([]Notification, error) {
	pending, err := PendingTasks(ctx, pool, "", DefaultTaskLookbackDays, today)
	if err != nil {
		return nil, err
	}
	todayStr := today.Format(dateLayout)

	out := []Notification{}
	for _, t := range pending {
		sev, detail := SeverityWarn, "Belum dikerjakan hari ini"
		if t.Date != todayStr {
			days := int(today.Sub(parseDateOrZero(t.Date)).Hours() / 24)
			sev = SeverityAlert
			detail = fmt.Sprintf("Belum dikerjakan — %s (%s)", formatIndoDate(t.Date), plural(days, "hari"))
		}
		out = append(out, Notification{
			ID:       fmt.Sprintf("task:%s:%s:%s", t.DefinitionID, t.BranchID, t.Date),
			Kind:     "daily_task",
			Severity: sev,
			Title:    t.Label(),
			Detail:   detail,
			Link:     t.LinkPath,
		})
	}
	return out, nil
}

func lowStockNotification(ctx context.Context, pool *pgxpool.Pool) ([]Notification, error) {
	// min_stock is denominated in the item's base unit, the same unit inventory
	// is held in, so the comparison needs no conversion. 0 means "no threshold
	// set", not "any stock is fine".
	var count int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM (
		  SELECT i.id
		  FROM items i
		  LEFT JOIN inventory inv ON inv.item_id = i.id
		  WHERE i.is_stock AND i.min_stock > 0
		  GROUP BY i.id, i.min_stock
		  HAVING COALESCE(SUM(inv.quantity), 0) < i.min_stock
		) t`).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return []Notification{}, nil
	}
	return []Notification{{
		ID:       "low_stock",
		Kind:     "low_stock",
		Severity: SeverityWarn,
		Title:    "Stok menipis",
		Detail:   fmt.Sprintf("%s di bawah stok minimum", plural(count, "barang")),
		Link:     "/items?low_stock=true",
		Count:    count,
	}}, nil
}

// CashVarianceLookbackDays bounds how far back an unexplained cash difference
// keeps being reported. Beyond a fortnight it is a question for the books, not
// something anyone can still resolve by remembering the day.
const CashVarianceLookbackDays = 14

// cashVarianceNotifications reports days whose counted cash did not match what
// the system says should have been there — the till and the petty cash box
// alike, as two lines rather than one, because they are found and fixed in
// different places.
//
// Only *closed* days count. A day with an opening and no closing yet is simply
// not finished; nagging about it before the shift ends is noise, and the daily
// task board already tracks the omission.
func cashVarianceNotifications(ctx context.Context, pool *pgxpool.Pool, today time.Time) ([]Notification, error) {
	from := today.AddDate(0, 0, -CashVarianceLookbackDays)

	sources := []struct{ kind, table, title, link string }{
		{"cash_variance", "cash_day_counts", "Selisih kas laci", "/cash-tracking"},
		{"petty_cash_variance", "petty_cash_counts", "Selisih kas kecil", "/petty-cash"},
	}

	out := []Notification{}
	for _, s := range sources {
		var count int
		var total int64
		// #nosec G201 — table name comes from the closed literal list above.
		err := pool.QueryRow(ctx, fmt.Sprintf(`
			SELECT COUNT(*), COALESCE(SUM(ABS(variance)), 0)
			FROM %s
			WHERE variance IS NOT NULL AND variance <> 0
			  AND count_date BETWEEN $1 AND $2`, s.table), from, today).Scan(&count, &total)
		if err != nil {
			return nil, err
		}
		if count == 0 {
			continue
		}
		out = append(out, Notification{
			ID:       s.kind,
			Kind:     s.kind,
			Severity: SeverityWarn,
			Title:    s.title,
			Detail: fmt.Sprintf("%s dengan selisih, total Rp %s dalam %d hari terakhir",
				plural(count, "hari"), formatThousands(total), CashVarianceLookbackDays),
			Link:  s.link,
			Count: count,
		})
	}
	return out, nil
}

// formatThousands renders an amount with Indonesian thousand separators. The
// notification feed is read at a glance, and "1.250.000" is legible where
// "1250000" is not.
func formatThousands(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// approvalNotifications counts what is waiting on a manager's decision. Counts,
// not one entry per request: a manager with forty pending kasbons needs one line
// that says forty, not forty lines.
func approvalNotifications(ctx context.Context, pool *pgxpool.Pool) ([]Notification, error) {
	queues := []struct{ kind, table, title, link string }{
		{"kasbon", "kasbons", "Kasbon menunggu persetujuan", "/hr/approvals"},
		{"leave", "leave_requests", "Pengajuan cuti menunggu persetujuan", "/hr/approvals"},
		{"overtime", "overtime_requests", "Pengajuan lembur menunggu persetujuan", "/hr/approvals"},
	}
	out := []Notification{}
	for _, q := range queues {
		var count int
		// Table names are literals from the slice above, never user input.
		if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE status = 'pending'`, q.table)).Scan(&count); err != nil {
			return nil, err
		}
		if count == 0 {
			continue
		}
		out = append(out, Notification{
			ID:       "approval:" + q.kind,
			Kind:     "approval",
			Severity: SeverityWarn,
			Title:    q.title,
			Detail:   plural(count, "pengajuan"),
			Link:     q.link,
			Count:    count,
		})
	}
	return out, nil
}

// hrNotifications covers the people queues: contracts about to lapse and payroll
// lines nobody has checked in a period that is still open.
func hrNotifications(ctx context.Context, pool *pgxpool.Pool, today time.Time) ([]Notification, error) {
	out := []Notification{}

	var expiring int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM employees
		WHERE status = 'active' AND employment_type = 'contract'
		  AND contract_end_date IS NOT NULL
		  AND contract_end_date BETWEEN $1 AND $1 + INTERVAL '30 days'`, today).Scan(&expiring)
	if err != nil {
		return nil, err
	}
	if expiring > 0 {
		out = append(out, Notification{
			ID:       "hr:contracts",
			Kind:     "contract_expiry",
			Severity: SeverityWarn,
			Title:    "Kontrak akan berakhir",
			Detail:   fmt.Sprintf("%s dalam 30 hari ke depan", plural(expiring, "karyawan")),
			Link:     "/hr/employees",
			Count:    expiring,
		})
	}

	var unreviewed int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM payroll_lines l
		JOIN payroll_periods p ON p.id = l.payroll_period_id
		WHERE p.status = 'open' AND NOT l.reviewed`).Scan(&unreviewed)
	if err != nil {
		return nil, err
	}
	if unreviewed > 0 {
		out = append(out, Notification{
			ID:       "hr:payroll_review",
			Kind:     "payroll_review",
			Severity: SeverityInfo,
			Title:    "Slip gaji belum diperiksa",
			Detail:   fmt.Sprintf("%s pada periode berjalan", plural(unreviewed, "baris")),
			Link:     "/hr/payroll",
			Count:    unreviewed,
		})
	}

	return out, nil
}

// ── formatting ──────────────────────────────────────────────────────────────

var indoMonths = [...]string{"", "Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

func formatIndoDate(s string) string {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%d %s", t.Day(), indoMonths[int(t.Month())])
}

// plural is Indonesian-friendly: no plural suffix, just the count and the noun.
func plural(n int, noun string) string { return fmt.Sprintf("%d %s", n, noun) }

// parseDateOrZero parses a dateLayout string, returning the zero time if it
// cannot. Callers here pass dates that PendingTasks itself formatted with
// dateLayout, so a failure is not reachable in practice — hence no error. It is
// deliberately not called mustDate: it does not panic, and the package's tests
// own that name for the helper that does.
func parseDateOrZero(s string) time.Time {
	t, _ := time.Parse(dateLayout, s)
	return t
}
