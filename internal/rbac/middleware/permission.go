package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/pkg"
	"bedrock/internal/rbac/service"
)

// RequirePermission enforces a feature full_code (e.g. system_users:view).
// Resources marked super_admin_only always require is_super_admin.
func RequirePermission(perm *service.PermissionService, required string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := authmiddleware.GetUserID(c)
		if userID == 0 {
			pkg.Error(c, http.StatusUnauthorized, "未登录")
			return
		}
		isSuper := authmiddleware.IsSuperAdmin(c)
		if err := perm.CheckAccess(userID, isSuper, required); err != nil {
			if service.IsForbidden(err) {
				pkg.Error(c, http.StatusForbidden, err.Error())
				return
			}
			pkg.Error(c, http.StatusInternalServerError, "权限校验失败")
			return
		}
		c.Next()
	}
}

// RequirePermissionOrPATScope accepts JWT with the RBAC permission, or PAT with patScope.
func RequirePermissionOrPATScope(perm *service.PermissionService, required, patScope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if authmiddleware.IsPAT(c) {
			if err := authmiddleware.RequirePATScope(c, patScope); err != nil {
				pkg.Error(c, http.StatusForbidden, "token scope insufficient")
				return
			}
			c.Next()
			return
		}
		RequirePermission(perm, required)(c)
	}
}
