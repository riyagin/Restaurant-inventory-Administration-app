package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Kasbon (cash advance) business logic. Pure helpers (number generation, the
// 2-month resolution window, installment-split validation, and status-transition
// guards) are DB-free and unit-tested. ProcessKasbon performs the CoA debit in a
// transaction. The payroll hooks (GetPendingInstallments / MarkInstallmentDeducted
// / ResolveKasbonIfComplete) are consumed by prompt 08.

// PiutangKaryawanAccountNumber is the system asset account ("Piutang Karyawan",
// employee receivable) seeded by migration 011. Processing a kasbon credits this
// account by the disbursed amount while debiting the chosen fund source.
const PiutangKaryawanAccountNumber = 10300

// ErrPiutangAccountMissing is returned when the system receivable account cannot be
// found (migration 011 not applied). The handler maps it to a 500 with a clear
// Indonesian message.
var ErrPiutangAccountMissing = errors.New("akun sistem 'Piutang Karyawan' tidak ditemukan")

// ── Pure helpers (DB-free, testable) ─────────────────────────────────────────

// GenerateKasbonNumber returns the next kasbon number for the given year, given the
// current highest sequence among existing KSB-YYYY-NNNN numbers for that year. The
// sequence is zero-padded to at least 4 digits, e.g.
// GenerateKasbonNumber(2026, 0) -> "KSB-2026-0001".
func GenerateKasbonNumber(year int, maxSeqForYear int32) string {
	return fmt.Sprintf("KSB-%04d-%04d", year, maxSeqForYear+1)
}

// FirstOfMonth returns the first day of t's month at midnight UTC.
func FirstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// monthsBetween returns the whole-calendar-month difference (b - a) using only the
// year/month components, e.g. Jan→Mar = 2.
func monthsBetween(a, b time.Time) int {
	return (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month())
}

// ValidateResolutionWindow checks that resolutionMonth (treated as a first-of-month
// date) is no earlier than the request month and at most 2 calendar months after
// the request date's month. Returns nil when in-window.
func ValidateResolutionWindow(requestDate, resolutionMonth time.Time) error {
	reqMonth := FirstOfMonth(requestDate)
	resMonth := FirstOfMonth(resolutionMonth)
	diff := monthsBetween(reqMonth, resMonth)
	if diff < 0 {
		return errors.New("bulan penyelesaian tidak boleh sebelum bulan pengajuan")
	}
	if diff > 2 {
		return errors.New("bulan penyelesaian maksimal 2 bulan setelah bulan pengajuan")
	}
	return nil
}

// InstallmentInput is a single deduction installment: a first-of-month due date and
// its whole-rupiah amount.
type InstallmentInput struct {
	DueMonth time.Time
	Amount   int64
}

// ValidateInstallmentSplit validates a 1- or 2-installment deduction plan against
// the total and the 2-month resolution window measured from requestDate:
//   - 1 or 2 installments only,
//   - each amount > 0,
//   - amounts sum exactly to total,
//   - each due_month within [request month, request month + 2],
//   - (for 2 installments) distinct due months.
func ValidateInstallmentSplit(total int64, requestDate time.Time, installments []InstallmentInput) error {
	n := len(installments)
	if n < 1 || n > 2 {
		return errors.New("jumlah cicilan harus 1 atau 2")
	}
	var sum int64
	seen := map[string]bool{}
	for _, ins := range installments {
		if ins.Amount <= 0 {
			return errors.New("nominal cicilan harus lebih dari 0")
		}
		sum += ins.Amount
		if err := ValidateResolutionWindow(requestDate, ins.DueMonth); err != nil {
			return fmt.Errorf("bulan cicilan tidak valid: %w", err)
		}
		key := FirstOfMonth(ins.DueMonth).Format("2006-01")
		if seen[key] {
			return errors.New("bulan cicilan tidak boleh sama")
		}
		seen[key] = true
	}
	if sum != total {
		return errors.New("total cicilan harus sama dengan jumlah kasbon")
	}
	return nil
}

// ── Status-transition guards (pure) ──────────────────────────────────────────

// CanEdit reports whether a kasbon in the given status may be edited (pending only).
func CanEdit(status string) bool { return status == "pending" }

// CanCancel reports whether a kasbon may be cancelled (pending or approved).
func CanCancel(status string) bool { return status == "pending" || status == "approved" }

// CanApprove reports whether a kasbon may be approved/rejected (pending only).
func CanApprove(status string) bool { return status == "pending" }

// CanProcess reports whether a kasbon may be processed (approved only).
func CanProcess(status string) bool { return status == "approved" }

// ── DB-touching operations ───────────────────────────────────────────────────

// ProcessKasbon sets the kasbon to 'processed', records the optional evidence photo
// path, debits the fund-source account (decreasing its balance by amount) and posts
// a matching entry to the "Piutang Karyawan" system asset account (increasing it).
// Must run inside a transaction (qtx). Returns the updated kasbon.
func ProcessKasbon(ctx context.Context, qtx *db.Queries, kasbon *db.Kasbon, processedBy pgtype.UUID, evidencePath pgtype.Text) (*db.Kasbon, error) {
	piutang, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: PiutangKaryawanAccountNumber, Valid: true})
	if err != nil || piutang == nil {
		return nil, ErrPiutangAccountMissing
	}

	updated, err := qtx.SetKasbonProcessed(ctx, &db.SetKasbonProcessedParams{
		ProcessedBy:       processedBy,
		EvidencePhotoPath: evidencePath,
		ID:                kasbon.ID,
	})
	if err != nil {
		return nil, err
	}

	// Debit the fund source (cash/asset out): balance decreases by amount.
	if err := UpdateBalance(ctx, qtx, kasbon.FundSourceAccountID.Bytes, -kasbon.Amount); err != nil {
		return nil, err
	}
	// Credit the employee receivable (asset in): balance increases by amount.
	if err := UpdateBalance(ctx, qtx, piutang.ID.Bytes, kasbon.Amount); err != nil {
		return nil, err
	}

	return updated, nil
}

// ── Payroll hooks (consumed by prompt 08) ────────────────────────────────────

// GetPendingInstallments returns the pending kasbon installments due on or before
// the given month (first-of-month) for an employee whose kasbon is processed.
// Prompt 08 payroll calls this to deduct them from net pay.
func GetPendingInstallments(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, month time.Time) ([]*db.KasbonInstallment, error) {
	return qtx.ListPendingInstallmentsForEmployee(ctx, &db.ListPendingInstallmentsForEmployeeParams{
		EmployeeID: employeeID,
		DueMonth:   pgtype.Date{Time: FirstOfMonth(month), Valid: true},
	})
}

// MarkInstallmentDeducted marks an installment 'deducted' and links it to the
// payroll line that consumed it. Must run inside a transaction (qtx).
func MarkInstallmentDeducted(ctx context.Context, qtx *db.Queries, installmentID, payrollLineID pgtype.UUID) error {
	return qtx.MarkKasbonInstallmentDeducted(ctx, &db.MarkKasbonInstallmentDeductedParams{
		PayrollLineID: payrollLineID,
		ID:            installmentID,
	})
}

// ResolveKasbonIfComplete marks the kasbon 'resolved' once all its installments are
// deducted (no pending installments remain). It is a no-op when installments are
// still pending or the kasbon has no installments. Must run inside a transaction.
func ResolveKasbonIfComplete(ctx context.Context, qtx *db.Queries, kasbonID pgtype.UUID) error {
	total, err := qtx.CountInstallments(ctx, kasbonID)
	if err != nil {
		return err
	}
	if total == 0 {
		return nil
	}
	pending, err := qtx.CountPendingInstallments(ctx, kasbonID)
	if err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}
	return qtx.SetKasbonResolved(ctx, kasbonID)
}

// LastResolvedKasbon returns the employee's most recently resolved kasbon (date +
// amount), or (nil, nil) when the employee has never had a resolved kasbon.
func LastResolvedKasbon(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID) (*db.GetLastResolvedKasbonRow, error) {
	row, err := qtx.GetLastResolvedKasbon(ctx, employeeID)
	if err != nil {
		// No resolved kasbon yet → not an error for callers.
		return nil, nil
	}
	return row, nil
}
