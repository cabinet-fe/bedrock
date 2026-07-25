package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/pkg"
	"bedrock/internal/system/service"
)

type NotificationHandler struct {
	notifications *service.NotificationService
}

func NewNotificationHandler(notifications *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{notifications: notifications}
}

func (h *NotificationHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/notifications", authMW)
	g.GET("", h.List)
	g.PUT("/read-all", h.MarkAllRead)
	g.PUT("/:id/read", h.MarkRead)
}

func (h *NotificationHandler) List(c *gin.Context) {
	userID := authmiddleware.GetUserID(c)
	q := pkg.ParseListQuery(c)
	var isRead *bool
	if v := c.Query("is_read"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			pkg.Error(c, http.StatusBadRequest, "参数错误")
			return
		}
		isRead = &b
	}
	items, total, err := h.notifications.ListByUser(userID, isRead, q)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *NotificationHandler) MarkRead(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	userID := authmiddleware.GetUserID(c)
	if err := h.notifications.MarkRead(uint(id), userID); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "操作失败")
		return
	}
	pkg.Success(c, nil)
}

func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	userID := authmiddleware.GetUserID(c)
	if err := h.notifications.MarkAllRead(userID); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "操作失败")
		return
	}
	pkg.Success(c, nil)
}
