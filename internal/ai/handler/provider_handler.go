package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"bedrock/internal/ai/model"
	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/pkg"
)

func (h *Handler) ListProviders(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, err := h.providers.ListProviders(page, pageSize)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Paginated(c, items, total, page, pageSize)
}

func (h *Handler) CreateProvider(c *gin.Context) {
	var input model.ProviderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	item, err := h.providers.CreateProvider(authmiddleware.GetUserID(c), input)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Created(c, item)
}

func (h *Handler) GetProvider(c *gin.Context) {
	id, ok := parseParamUint(c, "id")
	if !ok {
		return
	}
	item, err := h.providers.GetProvider(id)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) UpdateProvider(c *gin.Context) {
	id, ok := parseParamUint(c, "id")
	if !ok {
		return
	}
	var input model.ProviderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	item, err := h.providers.UpdateProvider(id, input)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) DeleteProvider(c *gin.Context) {
	id, ok := parseParamUint(c, "id")
	if !ok {
		return
	}
	if err := h.providers.DeleteProvider(id); err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, gin.H{"deleted": true})
}

func (h *Handler) ListModels(c *gin.Context) {
	id, ok := parseParamUint(c, "id")
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, err := h.providers.ListModelsByProvider(id, page, pageSize)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Paginated(c, items, total, page, pageSize)
}

func (h *Handler) CreateModel(c *gin.Context) {
	id, ok := parseParamUint(c, "id")
	if !ok {
		return
	}
	var input model.ModelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	item, err := h.providers.CreateModel(id, input)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Created(c, item)
}

func (h *Handler) GetModel(c *gin.Context) {
	mid, ok := parseParamUint(c, "mid")
	if !ok {
		return
	}
	item, err := h.providers.GetModel(mid)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) UpdateModel(c *gin.Context) {
	mid, ok := parseParamUint(c, "mid")
	if !ok {
		return
	}
	var input model.ModelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	item, err := h.providers.UpdateModel(mid, input)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) DeleteModel(c *gin.Context) {
	mid, ok := parseParamUint(c, "mid")
	if !ok {
		return
	}
	if err := h.providers.DeleteModel(mid); err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, gin.H{"deleted": true})
}

func parseParamUint(c *gin.Context, key string) (uint, bool) {
	raw := c.Param(key)
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		pkg.Error(c, http.StatusBadRequest, "无效参数: "+key)
		return 0, false
	}
	return uint(id), true
}
