package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/config"
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

func (h *HREmployeesHandler) SetUploadsDir(dir string) {
	h.uploadsDir = dir
}

func (h *HREmployeesHandler) resolveUploadsDir() string {
	if h.uploadsDir != "" {
		return h.uploadsDir
	}
	return config.DefaultUploadsDir
}

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
	// DivisionID: omitted leaves it unchanged; null/"" clears it.
	DivisionID *string `json:"division_id"`
}

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
	DivisionID      pgtype.UUID `json:"division_id"`
	DivisionName    pgtype.Text `json:"division_name"`
	EmploymentType  string      `json:"employment_type"`
	ContractEndDate pgtype.Date `json:"contract_end_date"`
	HasFace         bool        `json:"has_face"`
	// Latest kiosk face photo, preferred over the HR-uploaded photo as the avatar.
	FacePhotoPath pgtype.Text `json:"face_photo_path"`
}

// List — GET /api/hr/employees
func (h *HREmployeesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	search := strings.TrimSpace(q.Get("q"))
	branchID := strings.TrimSpace(q.Get("branch_id"))
	if b, ok := scopedBranchString(r); ok {
		branchID = b
	}
	positionID := strings.TrimSpace(q.Get("position_id"))
	divisionFilter := strings.TrimSpace(q.Get("division_id"))
	status := strings.TrimSpace(q.Get("status"))
	employmentType := strings.TrimSpace(q.Get("employment_type"))
	face := strings.TrimSpace(q.Get("face"))

	// Whitelisted sort columns keep ORDER BY injection-safe.
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
	if divisionFilter == "none" {
		conds = append(conds, "e.division_id IS NULL")
	} else if divisionFilter != "" {
		if _, err := parseUUID(divisionFilter); err != nil {
			respondError(w, http.StatusBadRequest, "divisi tidak valid")
			return
		}
		args = append(args, divisionFilter)
		conds = append(conds, fmt.Sprintf("e.division_id = $%d::uuid", len(args)))
	}
	if status != "" && status != "all" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("e.status = $%d", len(args)))
	}
	if employmentType == "permanent" || employmentType == "contract" || employmentType == "probation" {
		args = append(args, employmentType)
		conds = append(conds, fmt.Sprintf("e.employment_type = $%d", len(args)))
	}
	if face == "enrolled" {
		conds = append(conds, "e.face_embedding IS NOT NULL")
	} else if face == "not" {
		conds = append(conds, "e.face_embedding IS NULL")
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
		    e.division_id, d.name AS division_name,
		    e.employment_type, e.contract_end_date,
		    (e.face_embedding IS NOT NULL) AS has_face,
		    (SELECT fp.photo_path FROM employee_face_photos fp WHERE fp.employee_id = e.id ORDER BY fp.captured_at DESC, fp.created_at DESC LIMIT 1) AS face_photo_path
		FROM employees e
		JOIN positions p ON p.id = e.position_id
		JOIN branches  b ON b.id = e.branch_id
		LEFT JOIN divisions d ON d.id = e.division_id
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
			&row.DivisionID, &row.DivisionName,
			&row.EmploymentType, &row.ContractEndDate,
			&row.HasFace, &row.FacePhotoPath,
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

	// A failed photo lookup only hides the photo.
	out := employeeDetailResponse{GetEmployeeByIDRow: emp}
	if path, at, ok, ferr := service.LatestFacePhoto(r.Context(), h.pool, emp.ID); ferr != nil {
		log.Printf("hr employee get: latest face photo failed: %v", ferr)
	} else if ok {
		out.FacePhotoPath = &path
		out.FacePhotoAt = &at
	}
	respondJSON(w, http.StatusOK, out)
}

type employeeDetailResponse struct {
	*db.GetEmployeeByIDRow
	FacePhotoPath *string    `json:"face_photo_path"`
	FacePhotoAt   *time.Time `json:"face_photo_at"`
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

	employmentType, contractEnd, err := normalizeEmployment(body.EmploymentType, body.ContractEndDate, joinDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	divisionID, err := resolveEmployeeDivision(ctx, h.pool, body.DivisionID, branchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

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
	if divisionID.Valid {
		if err := qtx.SetEmployeeDivision(ctx, &db.SetEmployeeDivisionParams{ID: newID, DivisionID: divisionID}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan divisi karyawan")
			return
		}
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

	employmentType, contractEnd, err := normalizeEmployment(body.EmploymentType, body.ContractEndDate, joinDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	var divisionID pgtype.UUID
	if body.DivisionID != nil {
		if divisionID, err = resolveEmployeeDivision(ctx, h.pool, body.DivisionID, branchID); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	// UpdateEmployee drops the division on a branch change; an explicit one is applied after.
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
	if body.DivisionID != nil {
		if err := qtx.SetEmployeeDivision(ctx, &db.SetEmployeeDivisionParams{ID: pgID, DivisionID: divisionID}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan divisi karyawan")
			return
		}
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

type resignBody struct {
	ResignDate string `json:"resign_date"` // "YYYY-MM-DD" — effective resignation date
}

// ContractAlerts — GET /api/hr/employees/contract-alerts?days=30
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

// AttendanceHistory — GET /api/hr/employees/:id/attendance-history (same builder as the kiosk)
func (h *HREmployeesHandler) AttendanceHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	if _, err := h.queries.GetEmployeeByID(ctx, pgID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	hist, err := service.GetAttendanceHistory(ctx, h.pool, h.queries, pgID, service.TodayWIB())
	if err != nil {
		log.Printf("attendance history (emp=%s): %v", id, err)
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat kehadiran")
		return
	}
	respondJSON(w, http.StatusOK, hist)
}
