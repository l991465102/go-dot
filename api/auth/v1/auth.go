package v1

import "github.com/lzw7758/go-dot/internal/auth"

type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type LoginRes = auth.Session
type RefreshRes = auth.Session
type ChangePasswordReq struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}
type MeRes struct {
	User auth.User `json:"user"`
}
