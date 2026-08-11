package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// TasksHandler serves the daily task board, the notification feed behind the
// navbar bell, and the staff KPIs measured against those tasks.
type TasksHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewTasksHandler(pool *pgxpool.Pool, queries *db.Queries) *TasksHandler {
	return &TasksHandler{pool: pool, queries: queries}
}

// today reads the server's local date. Task dates are business dates, not
// instants, so the location matters and UTC would roll over seven hours early
// for an Indonesian user.
func today() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// taskDate reads a YYYY-MM-DD parameter as a business date, falling back rather
// than erroring: these bound a dashboard view, and a typo should show the
// default window, not a 400. Distinct from helpers.parseDateParam, which builds
// a nullable pgtype.Date for report filters.
func taskDate(s string, fallback time.Time) time.Time {
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(s)); err == nil {
		return t
	}
	return fallback
}

// Board — GET /api/tasks/daily?from=&to=
// The whole grid, done and not, for the dashboard's task section.
func (h *TasksHandler) Board(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := today()
	to := taskDate(q.Get("to"), now)
	from := taskDate(q.Get("from"), to.AddDate(0, 0, -6))
	if from.After(to) {
		respondError(w, http.StatusBadRequest, "tanggal awal melewati tanggal akhir")
		return
	}

	board, err := service.TaskBoard(r.Context(), h.pool, q.Get("role"), from, to, now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil tugas harian")
		return
	}
	respondJSON(w, http.StatusOK, board)
}

// Pending — GET /api/tasks/pending?lookback=
func (h *TasksHandler) Pending(w http.ResponseWriter, r *http.Request) {
	lookback, _ := strconv.Atoi(r.URL.Query().Get("lookback"))
	pending, err := service.PendingTasks(r.Context(), h.pool, "", lookback, today())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil tugas tertunda")
		return
	}
	respondJSON(w, http.StatusOK, pending)
}

// Notifications — GET /api/notifications
func (h *TasksHandler) Notifications(w http.ResponseWriter, r *http.Request) {
	items, err := service.NotificationsFor(r.Context(), h.pool, middleware.RoleFromCtx(r.Context()), today())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil notifikasi")
		return
	}
	respondJSON(w, http.StatusOK, items)
}

// ── task definitions ────────────────────────────────────────────────────────

// ListDefinitions — GET /api/tasks/definitions
func (h *TasksHandler) ListDefinitions(w http.ResponseWriter, r *http.Request) {
	defs, err := service.ListTaskDefinitions(r.Context(), h.pool, "", false)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar tugas")
		return
	}
	respondJSON(w, http.StatusOK, defs)
}

type taskDefinitionBody struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	TaskType      string `json:"task_type"`
	Scope         string `json:"scope"`
	TargetRole    string `json:"target_role"`
	LinkPath      string `json:"link_path"`
	StartsOn      string `json:"starts_on"`
	DueOffsetDays int    `json:"due_offset_days"`
	GraceDays     int    `json:"grace_days"`
	IsActive      *bool  `json:"is_active"`
	SortOrder     int    `json:"sort_order"`
}

// startsOnValue turns the request's start date into a nullable DATE. A duty the
// user creates today defaults to starting today — nobody should open the app to
// a backlog of failures for a rule that did not exist yesterday. Clearing the
// field (explicit "always") is done by sending "always".
func (b *taskDefinitionBody) startsOnValue() pgtype.Date {
	v := strings.TrimSpace(b.StartsOn)
	if v == "always" {
		return pgtype.Date{}
	}
	if v == "" {
		return pgtype.Date{Time: today(), Valid: true}
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return pgtype.Date{Time: today(), Valid: true}
	}
	return pgtype.Date{Time: t, Valid: true}
}

func (b *taskDefinitionBody) normalize() string {
	b.Name = strings.TrimSpace(b.Name)
	b.TaskType = strings.TrimSpace(b.TaskType)
	b.Scope = strings.TrimSpace(b.Scope)
	b.TargetRole = strings.TrimSpace(b.TargetRole)
	if b.Name == "" {
		return "nama tugas wajib diisi"
	}
	switch b.TaskType {
	case service.TaskTypePurchasing, service.TaskTypePOSImport, service.TaskTypeManual:
	default:
		return "jenis tugas harus purchasing, pos_import, atau manual"
	}
	if b.Scope == "" {
		b.Scope = service.TaskScopeGlobal
	}
	if b.Scope != service.TaskScopeGlobal && b.Scope != service.TaskScopePerBranch {
		return "cakupan tugas harus global atau per_branch"
	}
	if b.TargetRole == "" {
		b.TargetRole = middleware.RoleStaff
	}
	if b.GraceDays < 0 {
		return "toleransi keterlambatan tidak boleh negatif"
	}
	if b.DueOffsetDays < 0 {
		return "jeda ketersediaan data tidak boleh negatif"
	}
	return ""
}

// CreateDefinition — POST /api/tasks/definitions
func (h *TasksHandler) CreateDefinition(w http.ResponseWriter, r *http.Request) {
	var body taskDefinitionBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if msg := body.normalize(); msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	active := true
	if body.IsActive != nil {
		active = *body.IsActive
	}

	var id pgtype.UUID
	err := h.pool.QueryRow(r.Context(), `
		INSERT INTO daily_task_definitions
		  (name, description, task_type, scope, target_role, link_path, starts_on, due_offset_days, grace_days, is_active, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id`,
		body.Name, body.Description, body.TaskType, body.Scope, body.TargetRole,
		body.LinkPath, body.startsOnValue(), body.DueOffsetDays, body.GraceDays, active, body.SortOrder).Scan(&id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan tugas")
		return
	}

	logMutation(r, h.queries, "CREATE", "daily_task", uuid.UUID(id.Bytes),
		"Menambah tugas harian "+body.Name)
	respondJSON(w, http.StatusCreated, map[string]string{"id": uuid.UUID(id.Bytes).String()})
}

// UpdateDefinition — PUT /api/tasks/definitions/{id}
func (h *TasksHandler) UpdateDefinition(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tugas tidak valid")
		return
	}
	var body taskDefinitionBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if msg := body.normalize(); msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	active := true
	if body.IsActive != nil {
		active = *body.IsActive
	}

	tag, err := h.pool.Exec(r.Context(), `
		UPDATE daily_task_definitions
		SET name = $2, description = $3, task_type = $4, scope = $5, target_role = $6,
		    link_path = $7, starts_on = $8, due_offset_days = $9, grace_days = $10,
		    is_active = $11, sort_order = $12
		WHERE id = $1`,
		pgtype.UUID{Bytes: id, Valid: true}, body.Name, body.Description, body.TaskType,
		body.Scope, body.TargetRole, body.LinkPath, body.startsOnValue(), body.DueOffsetDays,
		body.GraceDays, active, body.SortOrder)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui tugas")
		return
	}
	if tag.RowsAffected() == 0 {
		respondError(w, http.StatusNotFound, "tugas tidak ditemukan")
		return
	}

	logMutation(r, h.queries, "UPDATE", "daily_task", id, "Memperbarui tugas harian "+body.Name)
	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteDefinition — DELETE /api/tasks/definitions/{id}
func (h *TasksHandler) DeleteDefinition(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tugas tidak valid")
		return
	}
	var name string
	err = h.pool.QueryRow(r.Context(),
		`DELETE FROM daily_task_definitions WHERE id = $1 RETURNING name`,
		pgtype.UUID{Bytes: id, Valid: true}).Scan(&name)
	if err != nil {
		respondError(w, http.StatusNotFound, "tugas tidak ditemukan")
		return
	}
	logMutation(r, h.queries, "DELETE", "daily_task", id, "Menghapus tugas harian "+name)
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// CompleteTask — POST /api/tasks/daily/complete
// Manual definitions only; derived ones are answered by the data.
func (h *TasksHandler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DefinitionID string `json:"definition_id"`
		BranchID     string `json:"branch_id"`
		Date         string `json:"date"`
		Note         string `json:"note"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if _, err := parseUUID(body.DefinitionID); err != nil {
		respondError(w, http.StatusBadRequest, "ID tugas tidak valid")
		return
	}
	date := taskDate(body.Date, today())

	var userID pgtype.UUID
	if uid := middleware.UserIDFromCtx(r.Context()); uid != uuid.Nil {
		userID = pgtype.UUID{Bytes: uid, Valid: true}
	}
	if err := service.CompleteManualTask(r.Context(), h.pool, body.DefinitionID, body.BranchID, date, userID, body.Note); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

// ── staff KPIs ──────────────────────────────────────────────────────────────

// ListKPIs — GET /api/hr/kpi
func (h *TasksHandler) ListKPIs(w http.ResponseWriter, r *http.Request) {
	kpis, err := service.ListStaffKPIs(r.Context(), h.pool, false)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar KPI")
		return
	}
	respondJSON(w, http.StatusOK, kpis)
}

type kpiBody struct {
	Name         string  `json:"name"`
	DefinitionID string  `json:"definition_id"`
	Metric       string  `json:"metric"`
	TargetValue  float64 `json:"target_value"`
	Weight       int     `json:"weight"`
	IsActive     *bool   `json:"is_active"`
}

func (b *kpiBody) normalize() string {
	b.Name = strings.TrimSpace(b.Name)
	b.Metric = strings.TrimSpace(b.Metric)
	if b.Name == "" {
		return "nama KPI wajib diisi"
	}
	if _, err := parseUUID(b.DefinitionID); err != nil {
		return "tugas harian wajib dipilih"
	}
	switch b.Metric {
	case service.KPIMetricCompletionRate, service.KPIMetricSameDayRate, service.KPIMetricCompletedCount:
	default:
		return "metrik harus completion_rate, same_day_rate, atau completed_count"
	}
	if b.TargetValue < 0 {
		return "target tidak boleh negatif"
	}
	if b.Weight <= 0 {
		b.Weight = 1
	}
	return ""
}

// CreateKPI — POST /api/hr/kpi
func (h *TasksHandler) CreateKPI(w http.ResponseWriter, r *http.Request) {
	var body kpiBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if msg := body.normalize(); msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	active := true
	if body.IsActive != nil {
		active = *body.IsActive
	}

	var id pgtype.UUID
	err := h.pool.QueryRow(r.Context(), `
		INSERT INTO staff_kpis (name, definition_id, metric, target_value, weight, is_active)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		body.Name, body.DefinitionID, body.Metric, body.TargetValue, body.Weight, active).Scan(&id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan KPI")
		return
	}

	logMutation(r, h.queries, "CREATE", "staff_kpi", uuid.UUID(id.Bytes), "Menambah KPI "+body.Name)
	respondJSON(w, http.StatusCreated, map[string]string{"id": uuid.UUID(id.Bytes).String()})
}

// UpdateKPI — PUT /api/hr/kpi/{id}
func (h *TasksHandler) UpdateKPI(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID KPI tidak valid")
		return
	}
	var body kpiBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if msg := body.normalize(); msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	active := true
	if body.IsActive != nil {
		active = *body.IsActive
	}

	tag, err := h.pool.Exec(r.Context(), `
		UPDATE staff_kpis
		SET name = $2, definition_id = $3, metric = $4, target_value = $5, weight = $6, is_active = $7
		WHERE id = $1`,
		pgtype.UUID{Bytes: id, Valid: true}, body.Name, body.DefinitionID, body.Metric,
		body.TargetValue, body.Weight, active)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui KPI")
		return
	}
	if tag.RowsAffected() == 0 {
		respondError(w, http.StatusNotFound, "KPI tidak ditemukan")
		return
	}

	logMutation(r, h.queries, "UPDATE", "staff_kpi", id, "Memperbarui KPI "+body.Name)
	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteKPI — DELETE /api/hr/kpi/{id}
func (h *TasksHandler) DeleteKPI(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID KPI tidak valid")
		return
	}
	var name string
	err = h.pool.QueryRow(r.Context(), `DELETE FROM staff_kpis WHERE id = $1 RETURNING name`,
		pgtype.UUID{Bytes: id, Valid: true}).Scan(&name)
	if err != nil {
		respondError(w, http.StatusNotFound, "KPI tidak ditemukan")
		return
	}
	logMutation(r, h.queries, "DELETE", "staff_kpi", id, "Menghapus KPI "+name)
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// KPIScores — GET /api/hr/kpi/scores?month=YYYY-MM
func (h *TasksHandler) KPIScores(w http.ResponseWriter, r *http.Request) {
	monthStr := strings.TrimSpace(r.URL.Query().Get("month"))
	month := today()
	if monthStr != "" {
		t, err := time.Parse("2006-01", monthStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format bulan harus YYYY-MM")
			return
		}
		month = t
	}

	cards, err := service.ScoreStaffKPIs(r.Context(), h.pool, month)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung skor KPI")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"month":      month.Format("2006-01"),
		"scorecards": cards,
	})
}
