package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type HREmployeesHandler struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	uploadsDir string
}

func NewHREmployeesHandler(pool *pgxpool.Pool, queries *db.Queries) *HREmployeesHandler {
	return &HREmployeesHandler{pool: pool, queries: queries}
}

// SetUploadsDir injects the uploads directory into the handler.
func (h *HREmployeesHandler) SetUploadsDir(dir string) {
	h.uploadsDir = dir
}

func (h *HREmployeesHandler) resolveUploadsDir() string {
	if h.uploadsDir != "" {
		return h.uploadsDir
	}
	return filepath.Join("..", "server", "uploads")
}

// ── Helpers ──────────────────────────────────────────────────────────────────

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

// employeeBody is the shared request payload for create/update.
type employeeBody struct {
	EmployeeCode      string `json:"employee_code"`
	FullName          string `json:"full_name"`
	Dob               string `json:"dob"`
	JoinDate          string `json:"join_date"`
	PositionID        string `json:"position_id"`
	BranchID          string `json:"branch_id"`
	Phone             string `json:"phone"`
	Email             string `json:"email"`
	Address           string `json:"address"`
	NationalID        string `json:"national_id"`
	BankName          string `json:"bank_name"`
	BankAccountNumber string `json:"bank_account_number"`
	BankAccountHolder string `json:"bank_account_holder"`
	UserID            string `json:"user_id"`
	Status            string `json:"status"`
	EmploymentType    string `json:"employment_type"`
	ContractEndDate   string `json:"contract_end_date"`
}

// normalizeEmployment validates the employment type + contract end date pair.
// A contract employee must have a contract end date; a permanent employee never
// carries one (any supplied date is cleared).
func normalizeEmployment(empType, endDate string) (string, pgtype.Date, error) {
	empType = strings.TrimSpace(empType)
	if empType == "" {
		empType = "permanent"
	}
	if empType != "permanent" && empType != "contract" {
		return "", pgtype.Date{}, fmt.Errorf("tipe kepegawaian harus tetap atau kontrak")
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

// employeeListRow is the projection returned by the List endpoint.
type employeeListRow struct {
	ID              pgtype.UUID `json:"id"`
	EmployeeCode    string      `json:"employee_code"`
	FullName        string      `json:"full_name"`
	JoinDate        pgtype.Date `json:"join_date"`
	Status          string      `json:"status"`
	PhotoPath       pgtype.Text `json:"photo_path"`
	PositionID      pgtype.UUID `json:"position_id"`
	PositionName    string      `json:"position_name"`
	BranchID        pgtype.UUID `json:"branch_id"`
	BranchName      string      `json:"branch_name"`
	EmploymentType  string      `json:"employment_type"`
	ContractEndDate pgtype.Date `json:"contract_end_date"`
}

// ── Employees ────────────────────────────────────────────────────────────────

// List — GET /api/hr/employees
func (h *HREmployeesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	search := strings.TrimSpace(q.Get("q"))
	branchID := strings.TrimSpace(q.Get("branch_id"))
	positionID := strings.TrimSpace(q.Get("position_id"))
	status := strings.TrimSpace(q.Get("status"))
	employmentType := strings.TrimSpace(q.Get("employment_type"))

	// Sorting — whitelist column + direction to keep the ORDER BY injection-safe.
	sortColumns := map[string]string{
		"name":      "e.full_name",
		"join_date": "e.join_date",
		"code":      "e.employee_code",
	}
	sortCol, ok := sortColumns[strings.TrimSpace(q.Get("sort"))]
	if !ok {
		sortCol = "e.full_name"
	}
	sortDir := "ASC"
	if strings.EqualFold(strings.TrimSpace(q.Get("dir")), "desc") {
		sortDir = "DESC"
	}
	// Deterministic tiebreaker so paging stays stable when the sort key ties.
	orderBy := sortCol + " " + sortDir
	if sortCol != "e.employee_code" {
		orderBy += ", e.employee_code ASC"
	}

	pageNum, pageSize := 1, 25
	if p := q.Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			pageNum = v
		}
	}
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	var args []any
	var conds []string

	if search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		n := len(args)
		conds = append(conds, fmt.Sprintf("(lower(e.full_name) LIKE $%d OR lower(e.employee_code) LIKE $%d)", n, n))
	}
	if branchID != "" {
		args = append(args, branchID)
		conds = append(conds, fmt.Sprintf("e.branch_id = $%d::uuid", len(args)))
	}
	if positionID != "" {
		args = append(args, positionID)
		conds = append(conds, fmt.Sprintf("e.position_id = $%d::uuid", len(args)))
	}
	if status != "" && status != "all" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("e.status = $%d", len(args)))
	}
	if employmentType == "permanent" || employmentType == "contract" {
		args = append(args, employmentType)
		conds = append(conds, fmt.Sprintf("e.employment_type = $%d", len(args)))
	}

	whereClause := ""
	if len(conds) > 0 {
		whereClause = "WHERE " + strings.Join(conds, " AND ")
	}

	args = append(args, pageSize)
	limitIdx := len(args)
	args = append(args, (pageNum-1)*pageSize)
	offsetIdx := len(args)

	listSQL := fmt.Sprintf(`
		SELECT
		    e.id, e.employee_code, e.full_name, e.join_date, e.status, e.photo_path,
		    e.position_id, p.name AS position_name,
		    e.branch_id, b.name AS branch_name,
		    e.employment_type, e.contract_end_date
		FROM employees e
		JOIN positions p ON p.id = e.position_id
		JOIN branches  b ON b.id = e.branch_id
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, whereClause, orderBy, limitIdx, offsetIdx)

	rows, err := h.pool.Query(ctx, listSQL, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	defer rows.Close()

	items := []employeeListRow{}
	for rows.Next() {
		var row employeeListRow
		if err := rows.Scan(
			&row.ID, &row.EmployeeCode, &row.FullName, &row.JoinDate, &row.Status, &row.PhotoPath,
			&row.PositionID, &row.PositionName,
			&row.BranchID, &row.BranchName,
			&row.EmploymentType, &row.ContractEndDate,
		); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data karyawan")
			return
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data karyawan")
		return
	}

	// Total count for pagination
	countSQL := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM employees e
		JOIN positions p ON p.id = e.position_id
		JOIN branches  b ON b.id = e.branch_id
		%s`, whereClause)
	var total int64
	if err := h.pool.QueryRow(ctx, countSQL, args[:len(args)-2]...).Scan(&total); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung data karyawan")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data":  items,
		"total": total,
		"page":  pageNum,
		"limit": pageSize,
	})
}

// Get — GET /api/hr/employees/:id
func (h *HREmployeesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	emp, err := h.queries.GetEmployeeByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	respondJSON(w, http.StatusOK, emp)
}

// Create — POST /api/hr/employees
func (h *HREmployeesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body employeeBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	body.FullName = strings.TrimSpace(body.FullName)
	if body.FullName == "" {
		respondError(w, http.StatusBadRequest, "nama lengkap wajib diisi")
		return
	}
	if strings.TrimSpace(body.PositionID) == "" {
		respondError(w, http.StatusBadRequest, "jabatan wajib dipilih")
		return
	}
	if strings.TrimSpace(body.BranchID) == "" {
		respondError(w, http.StatusBadRequest, "cabang wajib dipilih")
		return
	}

	positionID, err := uuidOrNull(body.PositionID)
	if err != nil || !positionID.Valid {
		respondError(w, http.StatusBadRequest, "jabatan tidak valid")
		return
	}
	branchID, err := uuidOrNull(body.BranchID)
	if err != nil || !branchID.Valid {
		respondError(w, http.StatusBadRequest, "cabang tidak valid")
		return
	}
	dob, err := dateOrNull(body.Dob)
	if err != nil {
		respondError(w, http.StatusBadRequest, "format tanggal lahir tidak valid")
		return
	}
	joinDate, err := dateOrNull(body.JoinDate)
	if err != nil || !joinDate.Valid {
		respondError(w, http.StatusBadRequest, "tanggal bergabung wajib diisi")
		return
	}
	userID, err := uuidOrNull(body.UserID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "akun pengguna tidak valid")
		return
	}

	status := strings.TrimSpace(body.Status)
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "inactive" && status != "resigned" {
		respondError(w, http.StatusBadRequest, "status harus active, inactive, atau resigned")
		return
	}

	employmentType, contractEnd, err := normalizeEmployment(body.EmploymentType, body.ContractEndDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	code := strings.TrimSpace(body.EmployeeCode)
	if code == "" {
		maxSeq, err := qtx.GetMaxEmployeeCodeSeq(ctx)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membuat kode karyawan")
			return
		}
		code = service.NextEmployeeCode(maxSeq)
	}

	newID, err := qtx.CreateEmployee(ctx, &db.CreateEmployeeParams{
		EmployeeCode:      code,
		FullName:          body.FullName,
		Dob:               dob,
		JoinDate:          joinDate,
		PositionID:        positionID,
		BranchID:          branchID,
		Phone:             textOrNull(body.Phone),
		Email:             textOrNull(body.Email),
		Address:           textOrNull(body.Address),
		NationalID:        textOrNull(body.NationalID),
		BankName:          textOrNull(body.BankName),
		BankAccountNumber: textOrNull(body.BankAccountNumber),
		BankAccountHolder: textOrNull(body.BankAccountHolder),
		UserID:            userID,
		Status:            status,
		EmploymentType:    employmentType,
		ContractEndDate:   contractEnd,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "kode karyawan sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat karyawan")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "hr_employee",
		EntityID:    newID.Bytes,
		Description: fmt.Sprintf("Membuat karyawan %s (%s)", body.FullName, code),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	emp, err := h.queries.GetEmployeeByID(ctx, newID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	respondJSON(w, http.StatusCreated, emp)
}

// Update — PUT /api/hr/employees/:id
func (h *HREmployeesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	var body employeeBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	body.FullName = strings.TrimSpace(body.FullName)
	body.EmployeeCode = strings.TrimSpace(body.EmployeeCode)
	if body.FullName == "" {
		respondError(w, http.StatusBadRequest, "nama lengkap wajib diisi")
		return
	}
	if body.EmployeeCode == "" {
		respondError(w, http.StatusBadRequest, "kode karyawan wajib diisi")
		return
	}

	positionID, err := uuidOrNull(body.PositionID)
	if err != nil || !positionID.Valid {
		respondError(w, http.StatusBadRequest, "jabatan tidak valid")
		return
	}
	branchID, err := uuidOrNull(body.BranchID)
	if err != nil || !branchID.Valid {
		respondError(w, http.StatusBadRequest, "cabang tidak valid")
		return
	}
	dob, err := dateOrNull(body.Dob)
	if err != nil {
		respondError(w, http.StatusBadRequest, "format tanggal lahir tidak valid")
		return
	}
	joinDate, err := dateOrNull(body.JoinDate)
	if err != nil || !joinDate.Valid {
		respondError(w, http.StatusBadRequest, "tanggal bergabung wajib diisi")
		return
	}
	userID, err := uuidOrNull(body.UserID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "akun pengguna tidak valid")
		return
	}

	status := strings.TrimSpace(body.Status)
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "inactive" && status != "resigned" {
		respondError(w, http.StatusBadRequest, "status harus active, inactive, atau resigned")
		return
	}

	employmentType, contractEnd, err := normalizeEmployment(body.EmploymentType, body.ContractEndDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	_, err = qtx.UpdateEmployee(ctx, &db.UpdateEmployeeParams{
		EmployeeCode:      body.EmployeeCode,
		FullName:          body.FullName,
		Dob:               dob,
		JoinDate:          joinDate,
		PositionID:        positionID,
		BranchID:          branchID,
		Phone:             textOrNull(body.Phone),
		Email:             textOrNull(body.Email),
		Address:           textOrNull(body.Address),
		NationalID:        textOrNull(body.NationalID),
		BankName:          textOrNull(body.BankName),
		BankAccountNumber: textOrNull(body.BankAccountNumber),
		BankAccountHolder: textOrNull(body.BankAccountHolder),
		UserID:            userID,
		Status:            status,
		EmploymentType:    employmentType,
		ContractEndDate:   contractEnd,
		ID:                pgID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "kode karyawan sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui karyawan")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "hr_employee",
		EntityID:    id,
		Description: fmt.Sprintf("Memperbarui karyawan %s (%s)", body.FullName, body.EmployeeCode),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	emp, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	respondJSON(w, http.StatusOK, emp)
}

type transitionPermanentBody struct {
	EffectiveDate string `json:"effective_date"` // "YYYY-MM-DD" — permanent-status date (THR day 0)
}

// TransitionToPermanent — POST /api/hr/employees/:id/transition-permanent
// Converts a contract (PKWT) employee to permanent (PKWTT), stamping the permanent
// status date. That date becomes the THR tenure "day 0" for the employee.
func (h *HREmployeesHandler) TransitionToPermanent(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body transitionPermanentBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	effDate, err := time.Parse("2006-01-02", strings.TrimSpace(body.EffectiveDate))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal efektif tidak valid")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	if emp.EmploymentType != service.EmploymentTypeContract {
		respondError(w, http.StatusConflict, "karyawan sudah berstatus tetap")
		return
	}
	if effDate.Before(emp.JoinDate.Time) {
		respondError(w, http.StatusBadRequest, "tanggal status tetap tidak boleh sebelum tanggal bergabung")
		return
	}

	if err := h.queries.TransitionEmployeeToPermanent(ctx, &db.TransitionEmployeeToPermanentParams{
		ID:             pgID,
		PermanentSince: pgtype.Date{Time: effDate, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengubah status karyawan")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "employee",
		EntityID:    id,
		Description: fmt.Sprintf("Mengubah status %s menjadi karyawan tetap (berlaku %s)", emp.FullName, effDate.Format("2006-01-02")),
	})

	updated, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

type resignBody struct {
	ResignDate string `json:"resign_date"` // "YYYY-MM-DD" — effective resignation date
}

// Resign — POST /api/hr/employees/:id/resign
// Marks an employee as resigned (mengundurkan diri), stamping the effective date.
// Preserves the row and its HR history (unlike a hard delete).
func (h *HREmployeesHandler) Resign(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body resignBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	resignDate, err := time.Parse("2006-01-02", strings.TrimSpace(body.ResignDate))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal resign tidak valid")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	if emp.Status == "resigned" {
		respondError(w, http.StatusConflict, "karyawan sudah berstatus resign")
		return
	}
	if resignDate.Before(emp.JoinDate.Time) {
		respondError(w, http.StatusBadRequest, "tanggal resign tidak boleh sebelum tanggal bergabung")
		return
	}

	if err := h.queries.ResignEmployee(ctx, &db.ResignEmployeeParams{
		ID:         pgID,
		ResignDate: pgtype.Date{Time: resignDate, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengubah status karyawan")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "employee",
		EntityID:    id,
		Description: fmt.Sprintf("Menandai %s sebagai resign (berlaku %s)", emp.FullName, resignDate.Format("2006-01-02")),
	})

	updated, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// Delete — DELETE /api/hr/employees/:id
func (h *HREmployeesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	if err := h.queries.DeleteEmployee(ctx, pgID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			respondError(w, http.StatusConflict, "karyawan masih dipakai data HR lain; ubah status menjadi nonaktif")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menghapus karyawan")
		return
	}

	// Remove photo file if present
	if emp.PhotoPath.Valid && emp.PhotoPath.String != "" {
		_ = os.Remove(filepath.Join(h.resolveUploadsDir(), emp.PhotoPath.String))
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "hr_employee",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus karyawan %s (%s)", emp.FullName, emp.EmployeeCode),
	})

	respondJSON(w, http.StatusOK, map[string]string{"message": "karyawan berhasil dihapus"})
}

// UploadPhoto — POST /api/hr/employees/:id/photo
func (h *HREmployeesHandler) UploadPhoto(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	if err := r.ParseMultipartForm(20 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "gagal membaca form upload")
		return
	}
	file, header, err := r.FormFile("photo")
	if err != nil {
		respondError(w, http.StatusBadRequest, "field 'photo' tidak ditemukan")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png":
	default:
		respondError(w, http.StatusBadRequest, "format file tidak didukung (jpg, jpeg, png)")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	filename := fmt.Sprintf("employee-%s-%d%s", id.String(), time.Now().Unix(), ext)
	uploadsDir := h.resolveUploadsDir()

	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat direktori upload")
		return
	}

	dst, err := os.Create(filepath.Join(uploadsDir, filename))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan file")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menulis file")
		return
	}

	// Delete old file if one exists
	if emp.PhotoPath.Valid && emp.PhotoPath.String != "" {
		_ = os.Remove(filepath.Join(uploadsDir, emp.PhotoPath.String))
	}

	if err := h.queries.UpdateEmployeePhotoPath(ctx, &db.UpdateEmployeePhotoPathParams{
		PhotoPath: pgtype.Text{String: filename, Valid: true},
		ID:        pgID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan path foto")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "hr_employee",
		EntityID:    id,
		Description: fmt.Sprintf("Upload foto karyawan %s", emp.FullName),
	})

	respondJSON(w, http.StatusOK, map[string]string{"photo_path": filename})
}

// DeletePhoto — DELETE /api/hr/employees/:id/photo
func (h *HREmployeesHandler) DeletePhoto(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	if !emp.PhotoPath.Valid || emp.PhotoPath.String == "" {
		respondError(w, http.StatusBadRequest, "karyawan tidak memiliki foto")
		return
	}

	_ = os.Remove(filepath.Join(h.resolveUploadsDir(), emp.PhotoPath.String))

	if err := h.queries.UpdateEmployeePhotoPath(ctx, &db.UpdateEmployeePhotoPathParams{
		PhotoPath: pgtype.Text{},
		ID:        pgID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus path foto")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "hr_employee",
		EntityID:    id,
		Description: fmt.Sprintf("Hapus foto karyawan %s", emp.FullName),
	})

	respondJSON(w, http.StatusOK, map[string]string{"message": "foto berhasil dihapus"})
}

// ContractAlerts — GET /api/hr/employees/contract-alerts
// Returns active contract employees whose contract ends within `days` (default
// 30 — i.e. the final month), including any already past due. Powers the
// expiring-contract notifier in the HR UI.
func (h *HREmployeesHandler) ContractAlerts(w http.ResponseWriter, r *http.Request) {
	days := 30
	if d := strings.TrimSpace(r.URL.Query().Get("days")); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v >= 0 && v <= 365 {
			days = v
		}
	}

	rows, err := h.queries.ListExpiringContracts(r.Context(), int32(days))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kontrak")
		return
	}
	if rows == nil {
		rows = []*db.ListExpiringContractsRow{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": rows, "days": days})
}

// ── Positions ────────────────────────────────────────────────────────────────

// ListPositions — GET /api/hr/positions
func (h *HREmployeesHandler) ListPositions(w http.ResponseWriter, r *http.Request) {
	positions, err := h.queries.ListPositions(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data jabatan")
		return
	}
	if positions == nil {
		positions = []*db.Position{}
	}
	respondJSON(w, http.StatusOK, positions)
}

// CreatePosition — POST /api/hr/positions
func (h *HREmployeesHandler) CreatePosition(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama jabatan wajib diisi")
		return
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	pos, err := h.queries.CreatePosition(r.Context(), &db.CreatePositionParams{
		Name:     body.Name,
		IsActive: isActive,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama jabatan sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat jabatan")
		return
	}

	_ = service.LogActivity(r.Context(), h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(r.Context()),
		Username:    middleware.UsernameFromCtx(r.Context()),
		Action:      "CREATE",
		EntityType:  "hr_position",
		EntityID:    pos.ID.Bytes,
		Description: fmt.Sprintf("Membuat jabatan %s", pos.Name),
	})

	respondJSON(w, http.StatusCreated, pos)
}

// UpdatePosition — PUT /api/hr/positions/:id
func (h *HREmployeesHandler) UpdatePosition(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama jabatan wajib diisi")
		return
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	pos, err := h.queries.UpdatePosition(r.Context(), &db.UpdatePositionParams{
		Name:     body.Name,
		IsActive: isActive,
		ID:       pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jabatan tidak ditemukan")
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama jabatan sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui jabatan")
		return
	}

	_ = service.LogActivity(r.Context(), h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(r.Context()),
		Username:    middleware.UsernameFromCtx(r.Context()),
		Action:      "UPDATE",
		EntityType:  "hr_position",
		EntityID:    id,
		Description: fmt.Sprintf("Memperbarui jabatan %s", pos.Name),
	})

	respondJSON(w, http.StatusOK, pos)
}

// DeletePosition — DELETE /api/hr/positions/:id
func (h *HREmployeesHandler) DeletePosition(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	pos, err := h.queries.GetPositionByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jabatan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data jabatan")
		return
	}

	count, err := h.queries.CountEmployeesByPosition(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa karyawan")
		return
	}
	if count > 0 {
		respondError(w, http.StatusConflict, "jabatan masih dipakai oleh karyawan")
		return
	}

	if err := h.queries.DeletePosition(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus jabatan")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "hr_position",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus jabatan %s", pos.Name),
	})

	respondJSON(w, http.StatusOK, map[string]string{"message": "jabatan berhasil dihapus"})
}
