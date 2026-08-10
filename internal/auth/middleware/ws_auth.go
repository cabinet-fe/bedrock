package middleware

import (
	"errors"
	"strings"

	authservice "bedrock/internal/auth/service"
)

var ErrWSAuthFailed = errors.New("websocket auth failed")

// WSUser is the authenticated identity for WebSocket query-token auth.
type WSUser struct {
	UserID       uint
	IsSuperAdmin bool
}

// ResolveQueryToken validates JWT or PAT (br_*) query tokens, matching REST AuthWithPAT.
func ResolveQueryToken(authSvc *authservice.AuthService, patSvc PATValidator, token string) (*WSUser, error) {
	if token == "" {
		return nil, ErrWSAuthFailed
	}
	if patSvc != nil && strings.HasPrefix(token, "br_") {
		userID, _, err := patSvc.ValidateBearer(token)
		if err != nil {
			return nil, ErrWSAuthFailed
		}
		user, err := authSvc.GetByID(userID)
		if err != nil || user == nil || !user.IsActive {
			return nil, ErrWSAuthFailed
		}
		return &WSUser{UserID: user.ID, IsSuperAdmin: user.IsSuperAdmin}, nil
	}
	claims, err := authSvc.ParseToken(token)
	if err != nil {
		return nil, ErrWSAuthFailed
	}
	user, err := authSvc.GetByID(claims.UserID)
	if err != nil || user == nil || !user.IsActive {
		return nil, ErrWSAuthFailed
	}
	return &WSUser{UserID: user.ID, IsSuperAdmin: user.IsSuperAdmin}, nil
}
