package middleware

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	authservice "bedrock/internal/auth/service"
	"bedrock/internal/pkg"
)

const (
	ctxUserID       = "user_id"
	ctxUsername     = "username"
	ctxIsSuperAdmin = "is_super_admin"
	ctxIsPAT        = "is_pat"
	ctxPATScopes    = "pat_scopes"
)

var ErrPATWrongScope = errors.New("token scope insufficient")

// PATValidator validates personal access tokens without importing the resource package
// (implemented by resource PATService; wired in cmd/server).
type PATValidator interface {
	ValidateBearer(raw string) (userID uint, scopes []string, err error)
}

// AuthWithPAT accepts JWT or PAT (br_*) under Authorization: Bearer.
func AuthWithPAT(authSvc *authservice.AuthService, patSvc PATValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			pkg.Error(c, http.StatusUnauthorized, "missing authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			pkg.Error(c, http.StatusUnauthorized, "invalid authorization header")
			return
		}
		raw := parts[1]

		if patSvc != nil && strings.HasPrefix(raw, "br_") {
			userID, scopes, err := patSvc.ValidateBearer(raw)
			if err != nil {
				pkg.Error(c, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			user, err := authSvc.GetByID(userID)
			if err != nil || user == nil || !user.IsActive {
				pkg.Error(c, http.StatusUnauthorized, "用户不存在或已被禁用")
				return
			}
			c.Set(ctxUserID, user.ID)
			c.Set(ctxUsername, user.Username)
			c.Set(ctxIsSuperAdmin, user.IsSuperAdmin)
			c.Set(ctxIsPAT, true)
			c.Set(ctxPATScopes, scopes)
			c.Next()
			return
		}

		claims, err := authSvc.ParseToken(raw)
		if err != nil {
			pkg.Error(c, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		user, err := authSvc.GetByID(claims.UserID)
		if err != nil || user == nil || !user.IsActive {
			pkg.Error(c, http.StatusUnauthorized, "用户不存在或已被禁用")
			return
		}

		c.Set(ctxUserID, claims.UserID)
		c.Set(ctxUsername, claims.Username)
		c.Set(ctxIsSuperAdmin, user.IsSuperAdmin)
		c.Set(ctxIsPAT, false)
		c.Next()
	}
}

func GetUserID(c *gin.Context) uint {
	v, ok := c.Get(ctxUserID)
	if !ok {
		return 0
	}
	if id, ok := v.(uint); ok {
		return id
	}
	return 0
}

func GetUsername(c *gin.Context) string {
	v, ok := c.Get(ctxUsername)
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func IsSuperAdmin(c *gin.Context) bool {
	v, ok := c.Get(ctxIsSuperAdmin)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func IsPAT(c *gin.Context) bool {
	v, ok := c.Get(ctxIsPAT)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func PATScopes(c *gin.Context) []string {
	v, ok := c.Get(ctxPATScopes)
	if !ok {
		return nil
	}
	scopes, _ := v.([]string)
	return scopes
}

func RequirePATScope(c *gin.Context, required string) error {
	if slices.Contains(PATScopes(c), required) {
		return nil
	}
	return ErrPATWrongScope
}
