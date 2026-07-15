package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	v1 "github.com/lzw7758/go-dot/api/auth/v1"
	"github.com/lzw7758/go-dot/internal/auth"
)

const refreshCookieName = "go_dot_refresh"

type AuthController struct {
	service      *auth.Service
	secureCookie bool
}

func NewAuthController(service *auth.Service, secureCookie bool) AuthController {
	return AuthController{service: service, secureCookie: secureCookie}
}
func (c AuthController) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/login", c.login)
	mux.HandleFunc("POST /api/v1/auth/refresh", c.refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", c.logout)
	mux.HandleFunc("GET /api/v1/auth/me", c.me)
	mux.HandleFunc("POST /api/v1/auth/change-password", c.changePassword)
}
func (c AuthController) login(w http.ResponseWriter, r *http.Request) {
	var req v1.LoginReq
	if !decodeJSON(w, r, &req) {
		return
	}
	session, err := c.service.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
		} else {
			writeError(w, http.StatusInternalServerError, "authentication service unavailable")
		}
		return
	}
	c.setRefreshCookie(w, session.RefreshToken)
	writeJSON(w, http.StatusOK, session)
}
func (c AuthController) refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	session, err := c.service.Refresh(r.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidToken) {
			writeError(w, http.StatusUnauthorized, "invalid refresh token")
		} else {
			writeError(w, http.StatusInternalServerError, "authentication service unavailable")
		}
		return
	}
	c.setRefreshCookie(w, session.RefreshToken)
	writeJSON(w, http.StatusOK, session)
}
func (c AuthController) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(refreshCookieName); err == nil {
		if err := c.service.Logout(r.Context(), cookie.Value); err != nil && !errors.Is(err, auth.ErrInvalidToken) {
			writeError(w, http.StatusInternalServerError, "authentication service unavailable")
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: refreshCookieName, Value: "", Path: "/api/v1/auth", MaxAge: -1, HttpOnly: true, Secure: c.secureCookie, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}
func (c AuthController) me(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	u, err := c.service.User(r.Context(), p.UserID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
		} else {
			writeError(w, http.StatusInternalServerError, "authentication service unavailable")
		}
		return
	}
	u.PasswordHash = ""
	writeJSON(w, http.StatusOK, v1.MeRes{User: u})
}
func (c AuthController) changePassword(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req v1.ChangePasswordReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := c.service.ChangePassword(r.Context(), p.UserID, req.CurrentPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "invalid credentials")
		case errors.Is(err, auth.ErrInvalidPassword):
			writeError(w, http.StatusBadRequest, "invalid new password")
		default:
			writeError(w, http.StatusInternalServerError, "authentication service unavailable")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (c AuthController) setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: refreshCookieName, Value: token, Path: "/api/v1/auth", MaxAge: 7 * 24 * 60 * 60, HttpOnly: true, Secure: c.secureCookie, SameSite: http.SameSiteLaxMode})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
