package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/system/service"
)

// MailHandler exposes system-level SMTP mail config endpoints.
type MailHandler struct {
	mail *service.MailService
	perm *rbacservice.PermissionService
}

func NewMailHandler(mail *service.MailService, perm *rbacservice.PermissionService) *MailHandler {
	return &MailHandler{mail: mail, perm: perm}
}

// RegisterRoutes mounts SMTP config endpoints under system settings admin permissions.
func (h *MailHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/system/mail", authMW)
	g.GET("/smtp", rbacmw.RequirePermission(h.perm, "system_settings:view"), h.GetConfig)
	g.PUT("/smtp", rbacmw.RequirePermission(h.perm, "system_settings:update"), h.SaveConfig)
	g.POST("/smtp/test", rbacmw.RequirePermission(h.perm, "system_settings:update"), h.SendTest)
}

func (h *MailHandler) GetConfig(c *gin.Context) {
	view, err := h.mail.GetConfig()
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询 SMTP 配置失败")
		return
	}
	pkg.Success(c, view)
}

func (h *MailHandler) SaveConfig(c *gin.Context) {
	var req struct {
		Host        string `json:"host" binding:"required"`
		Port        int    `json:"port" binding:"required"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		FromAddress string `json:"from_address" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	view, err := h.mail.SaveConfig(service.MailSMTPConfigInput{
		Host:        req.Host,
		Port:        req.Port,
		Username:    req.Username,
		Password:    req.Password,
		FromAddress: req.FromAddress,
	})
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, view)
}

func (h *MailHandler) SendTest(c *gin.Context) {
	var req struct {
		To string `json:"to" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	result, err := h.mail.SendTest(req.To)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, result)
}
