package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"bedrock/internal/ops/service"
	"bedrock/internal/pkg"
)

func (h *OpsHandler) ListSiteLinks(c *gin.Context) {
	items, err := h.siteLinks.List()
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询网站链接失败")
		return
	}
	pkg.Success(c, items)
}

func (h *OpsHandler) CreateSiteLink(c *gin.Context) {
	var input service.SiteLinkInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效网站链接")
		return
	}
	item, err := h.siteLinks.Create(input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, item)
}

func (h *OpsHandler) UpdateSiteLink(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input service.SiteLinkInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效网站链接")
		return
	}
	item, err := h.siteLinks.Update(id, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *OpsHandler) DeleteSiteLink(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if err := h.siteLinks.Delete(id); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": id})
}
