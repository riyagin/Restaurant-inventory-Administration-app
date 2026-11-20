package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/service"
)

func textOrNull(s string) pgtype.Text {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func dateOrNull(s string) (pgtype.Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.Date{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return pgtype.Date{}, err
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

func uuidOrNull(s string) (pgtype.UUID, error) {
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

// resolveEmployeeDivision rejects a division of another branch.
func resolveEmployeeDivision(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, raw *string, branchID pgtype.UUID) (pgtype.UUID, error) {
	id, err := optUUID(raw)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("divisi tidak valid")
	}
	if !id.Valid {
		return id, nil
	}
	var divBranch pgtype.UUID
	if err := q.QueryRow(ctx, `SELECT branch_id FROM divisions WHERE id = $1`, id).Scan(&divBranch); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, fmt.Errorf("divisi tidak ditemukan")
		}
		return pgtype.UUID{}, fmt.Errorf("gagal memeriksa divisi")
	}
	if divBranch != branchID {
		return pgtype.UUID{}, fmt.Errorf("divisi bukan milik cabang karyawan ini")
	}
	return id, nil
}

// normalizeEmployment: a contract needs an end date, permanent has none, and
// probation always ends join date + 1 month.
func normalizeEmployment(empType, endDate string, joinDate pgtype.Date) (string, pgtype.Date, error) {
	empType = strings.TrimSpace(empType)
	if empType == "" {
		empType = service.EmploymentTypePermanent
	}
	if empType == service.EmploymentTypeProbation {
		if !joinDate.Valid {
			return "", pgtype.Date{}, fmt.Errorf("tanggal bergabung wajib diisi untuk masa percobaan")
		}
		return empType, pgtype.Date{Time: service.ProbationEndDate(joinDate.Time), Valid: true}, nil
	}
	if empType != service.EmploymentTypePermanent && empType != service.EmploymentTypeContract {
		return "", pgtype.Date{}, fmt.Errorf("tipe kepegawaian harus tetap, kontrak, atau masa percobaan")
	}
	contractEnd, err := dateOrNull(endDate)
	if err != nil {
		return "", pgtype.Date{}, fmt.Errorf("format tanggal berakhir kontrak tidak valid")
	}
	if empType == "contract" && !contractEnd.Valid {
		return "", pgtype.Date{}, fmt.Errorf("tanggal berakhir kontrak wajib diisi untuk karyawan kontrak")
	}
	if empType == "permanent" {
		contractEnd = pgtype.Date{}
	}
	return empType, contractEnd, nil
}
