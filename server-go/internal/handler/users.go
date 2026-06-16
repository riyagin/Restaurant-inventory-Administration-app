package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
)

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
	if body.Role != "admin" && body.Role != "manager" && body.Role != "staff" {
		respondError(w, http.StatusBadRequest, "role harus admin, manager, atau staff")
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
	if body.Role != "admin" && body.Role != "manager" && body.Role != "staff" {
		respondError(w, http.StatusBadRequest, "role harus admin, manager, atau staff")
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
	}

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

	if err := h.queries.DeleteUser(r.Context(), pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus pengguna")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "pengguna berhasil dihapus"})
}
