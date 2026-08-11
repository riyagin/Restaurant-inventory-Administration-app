package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
)

// validUserRoles enumerates the roles an admin may assign.
//
//   - superuser       — every capability, approvals included (middleware.hasRole
//     bypasses each gate for it).
//   - admin / manager — full access; only manager may approve requests.
//   - staff           — same reach as admin except reports and the activity log.
//   - hr              — confined to the HR module (middleware.RestrictHRRole).
//   - store_manager   — scoped to HR attendance record entry/correction only,
//     see middleware.RequireAttendanceAccess.
var validUserRoles = map[string]bool{
	middleware.RoleSuperuser:    true,
	middleware.RoleAdmin:        true,
	middleware.RoleManager:      true,
	middleware.RoleStaff:        true,
	middleware.RoleHR:           true,
	middleware.RoleStoreManager: true,
}

type UsersHandler struct {
	queries *db.Queries
}

func NewUsersHandler(queries *db.Queries) *UsersHandler {
	return &UsersHandler{queries: queries}
}

func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.queries.ListUsers(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pengguna")
		return
	}
	if users == nil {
		users = []*db.ListUsersRow{}
	}
	respondJSON(w, http.StatusOK, users)
}

func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Username = strings.ToLower(strings.TrimSpace(body.Username))
	if body.Username == "" || body.Password == "" || body.Role == "" {
		respondError(w, http.StatusBadRequest, "username, password, dan role wajib diisi")
		return
	}
	if !validUserRoles[body.Role] {
		respondError(w, http.StatusBadRequest, "role harus superuser, admin, manager, staff, hr, atau store_manager")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), 12)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses password")
		return
	}

	user, err := h.queries.CreateUser(r.Context(), &db.CreateUserParams{
		Username:     body.Username,
		PasswordHash: string(hash),
		Role:         body.Role,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat pengguna")
		return
	}

	logMutation(r, h.queries, "CREATE", "user", user.ID.Bytes,
		fmt.Sprintf("Menambahkan pengguna %q dengan role %s", body.Username, body.Role))

	respondJSON(w, http.StatusCreated, user)
}

func (h *UsersHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	var body struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Username = strings.ToLower(strings.TrimSpace(body.Username))
	if body.Username == "" || body.Role == "" {
		respondError(w, http.StatusBadRequest, "username dan role wajib diisi")
		return
	}
	if !validUserRoles[body.Role] {
		respondError(w, http.StatusBadRequest, "role harus superuser, admin, manager, staff, hr, atau store_manager")
		return
	}

	before, err := h.queries.GetUserByID(r.Context(), pgID)
	if err != nil {
		respondError(w, http.StatusNotFound, "pengguna tidak ditemukan")
		return
	}

	user, err := h.queries.UpdateUser(r.Context(), &db.UpdateUserParams{
		Username: body.Username,
		Role:     body.Role,
		ID:       pgID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui pengguna")
		return
	}

	// A role change grants or revokes access, and a password reset takes over an
	// account — both need to be attributable. The password itself is never logged.
	var changes []string
	if before.Username != body.Username {
		changes = append(changes, fmt.Sprintf("username: %q → %q", before.Username, body.Username))
	}
	if before.Role != body.Role {
		changes = append(changes, fmt.Sprintf("role: %s → %s", before.Role, body.Role))
	}

	if body.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), 12)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses password")
			return
		}
		if err := h.queries.UpdateUserPassword(r.Context(), &db.UpdateUserPasswordParams{
			PasswordHash: string(hash),
			ID:           pgID,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui password")
			return
		}
		changes = append(changes, "password direset")
	}

	logMutation(r, h.queries, "UPDATE", "user", id,
		fmt.Sprintf("Mengubah pengguna %q%s", before.Username, changeClause(changes)))

	respondJSON(w, http.StatusOK, user)
}

func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	callerID := middleware.UserIDFromCtx(r.Context())
	if id == callerID {
		respondError(w, http.StatusBadRequest, "tidak dapat menghapus akun sendiri")
		return
	}

	existing, err := h.queries.GetUserByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "pengguna tidak ditemukan")
		return
	}

	if err := h.queries.DeleteUser(r.Context(), pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus pengguna")
		return
	}

	logMutation(r, h.queries, "DELETE", "user", id,
		fmt.Sprintf("Menghapus pengguna %q (role %s)", existing.Username, existing.Role))

	respondJSON(w, http.StatusOK, map[string]string{"message": "pengguna berhasil dihapus"})
}
