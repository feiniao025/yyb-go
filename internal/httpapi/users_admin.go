package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// handleAdminListUsers returns all registered users for the admin.
func (a *App) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	users, err := a.db.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]any{
			"id":         u.ID,
			"username":   u.Username,
			"role":       u.Role,
			"disabled":   u.Disabled,
			"created_at": u.CreatedAt,
			"updated_at": u.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAdminDisableUser disables a registered user, invalidating all sessions.
func (a *App) handleAdminDisableUser(w http.ResponseWriter, r *http.Request) {
	a.handleAdminSetUserDisabled(w, r, true)
}

// handleAdminEnableUser enables a registered user.
func (a *App) handleAdminEnableUser(w http.ResponseWriter, r *http.Request) {
	a.handleAdminSetUserDisabled(w, r, false)
}

func (a *App) handleAdminSetUserDisabled(w http.ResponseWriter, r *http.Request, disabled bool) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := adminUserIDFromPath(w, r)
	if !ok {
		return
	}
	u, err := a.db.GetUser(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if err := a.db.SetUserDisabled(r.Context(), id, disabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	action := "enabled"
	if disabled {
		action = "disabled"
	}
	// Disabling invalidates all active sessions.
	if disabled {
		a.invalidateUserSessions(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       id,
		"username": u.Username,
		"disabled": disabled,
		"action":   action,
	})
}

// handleAdminResetPassword resets a user's password, invalidating their sessions
// while keeping their API token unchanged.
func (a *App) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := adminUserIDFromPath(w, r)
	if !ok {
		return
	}
	var body struct {
		NewPassword string `json:"new_password"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	body.NewPassword = strings.TrimSpace(body.NewPassword)
	if len(body.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "新密码至少需要 6 位")
		return
	}
	u, err := a.db.GetUser(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if err := a.db.UpdateUserPassword(r.Context(), id, hashPassword(body.NewPassword)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.invalidateUserSessions(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       id,
		"username": u.Username,
		"changed":  true,
	})
}

// handleAdminDeleteUser removes a user and all accounts owned by that user.
func (a *App) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := adminUserIDFromPath(w, r)
	if !ok {
		return
	}
	u, err := a.db.GetUser(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if err := a.db.DeleteUser(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.invalidateUserSessions(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       id,
		"username": u.Username,
		"deleted":  true,
	})
}

// adminUserIDFromPath extracts the user id from /admin/users/{id}/... paths.
func adminUserIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	rest := strings.TrimPrefix(r.URL.Path, "/admin/users/")
	idStr := rest
	if i := strings.Index(rest, "/"); i >= 0 {
		idStr = rest[:i]
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return 0, false
	}
	return id, true
}
