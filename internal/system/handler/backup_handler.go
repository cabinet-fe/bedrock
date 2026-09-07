package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/system/model"
	"bedrock/internal/system/service"
)

// BackupHandler exposes endpoints for managing system backups and recovery.
type BackupHandler struct {
	backups *service.BackupService
	perm    *rbacservice.PermissionService
}

// NewBackupHandler constructs a new BackupHandler.
func NewBackupHandler(backups *service.BackupService, perm *rbacservice.PermissionService) *BackupHandler {
	return &BackupHandler{
		backups: backups,
		perm:    perm,
	}
}

// RegisterRoutes registers backup endpoints and binds RBAC permissions.
func (h *BackupHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/system/backups", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "system_backup:view"), h.List)
	g.POST("", rbacmw.RequirePermission(h.perm, "system_backup:create"), h.Create)
	g.GET("/:id/download", rbacmw.RequirePermission(h.perm, "system_backup:download"), h.Download)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "system_backup:delete"), h.Delete)
	g.POST("/inspect", rbacmw.RequirePermission(h.perm, "system_backup:restore"), h.Inspect)
	g.POST("/restore", rbacmw.RequirePermission(h.perm, "system_backup:restore"), h.Restore)
}

// List lists backup records with pagination.
func (h *BackupHandler) List(c *gin.Context) {
	q := pkg.ParseListQuery(c)
	items, total, err := h.backups.List(q)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询备份记录失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

// Create triggers a one-click backup operation.
func (h *BackupHandler) Create(c *gin.Context) {
	var req model.SystemBackupCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	userID := authmiddleware.GetUserID(c)
	backup, err := h.backups.CreateBackup(userID, req)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Created(c, backup)
}

// Download streams the physical backup zip archive.
func (h *BackupHandler) Download(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	filePath, filename, err := h.backups.GetDownloadPath(uint(id))
	if err != nil {
		if errors.Is(err, service.ErrBackupNotFound) || strings.Contains(err.Error(), "不存在") {
			pkg.Error(c, http.StatusNotFound, err.Error())
		} else {
			pkg.Error(c, http.StatusBadRequest, err.Error())
		}
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Header("Content-Type", "application/zip")
	c.File(filePath)
}

// Delete deletes a backup database record and its physical archive file.
func (h *BackupHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := h.backups.DeleteBackup(uint(id)); err != nil {
		if errors.Is(err, service.ErrBackupNotFound) {
			pkg.Error(c, http.StatusNotFound, "备份记录不存在")
		} else {
			pkg.Error(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	pkg.Success(c, nil)
}

// Inspect accepts an uploaded zip archive and returns inspected manifest and compatibility result.
func (h *BackupHandler) Inspect(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "请提供备份 zip 文件")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无法读取上传文件")
		return
	}
	defer file.Close()

	result, err := h.backups.InspectUploadedBackup(fileHeader.Filename, file, fileHeader.Size)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, result)
}

// Restore executes online restoration from an existing backup or uploaded token.
func (h *BackupHandler) Restore(c *gin.Context) {
	var req model.SystemBackupRestoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	userID := authmiddleware.GetUserID(c)
	if userID == 0 {
		pkg.Error(c, http.StatusUnauthorized, "未登录")
		return
	}
	resp, err := h.backups.Restore(userID, req)
	if err != nil {
		if errors.Is(err, service.ErrBackupNotFound) {
			pkg.Error(c, http.StatusNotFound, err.Error())
		} else {
			pkg.Error(c, http.StatusBadRequest, err.Error())
		}
		return
	}
	pkg.Success(c, resp)
}
