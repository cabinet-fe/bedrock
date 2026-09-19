package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/service"
	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/harness/provider"
	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	storageservice "bedrock/internal/storage/service"
)

// HarnessCatalog is the harness directory surface the ai handler passes
// through (satisfied by the harness Provider).
type HarnessCatalog interface {
	ListModels(ctx context.Context, directory string) ([]provider.ModelInfo, error)
	ListAgents(ctx context.Context, directory string) ([]provider.AgentInfo, error)
}

type Handler struct {
	agents              *service.AgentService
	skills              *service.SkillService
	perm                *rbacservice.PermissionService
	providers           *service.ProviderService
	chat                *ChatHandler
	harness             HarnessCatalog
	chatCompletionsAuth gin.HandlerFunc
}

func NewHandler(
	agents *service.AgentService,
	skills *service.SkillService,
	perm *rbacservice.PermissionService,
	providers *service.ProviderService,
	chat ...*ChatHandler,
) *Handler {
	h := &Handler{agents: agents, skills: skills, perm: perm, providers: providers}
	if len(chat) > 0 {
		h.chat = chat[0]
	}
	return h
}

// SetHarnessCatalog wires the harness directory passthrough
// (harness.enabled=false leaves it unset: the endpoints answer 503).
func (h *Handler) SetHarnessCatalog(c HarnessCatalog) { h.harness = c }

func (h *Handler) SetChatHandler(chat *ChatHandler) {
	h.chat = chat
}

// SetChatCompletionsAuth overrides auth for POST /ai/chat/completions (JWT/PAT
// or loopback harness token). When unset, RegisterRoutes uses authMW.
func (h *Handler) SetChatCompletionsAuth(mw gin.HandlerFunc) {
	h.chatCompletionsAuth = mw
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	ai := rg.Group("/ai", authMW)
	ai.GET("/agents", rbacmw.RequirePermission(h.perm, "ai_agents:view"), h.ListAgents)
	ai.POST("/agents", rbacmw.RequirePermission(h.perm, "ai_agents:create"), h.CreateAgent)
	ai.GET("/agents/:id", rbacmw.RequirePermission(h.perm, "ai_agents:view"), h.GetAgent)
	ai.PUT("/agents/:id", rbacmw.RequirePermission(h.perm, "ai_agents:update"), h.UpdateAgent)
	ai.DELETE("/agents/:id", rbacmw.RequirePermission(h.perm, "ai_agents:delete"), h.DeleteAgent)
	ai.GET("/agents/:id/triggers", rbacmw.RequirePermission(h.perm, "ai_agents:view"), h.ListTriggers)
	ai.POST("/agents/:id/triggers", rbacmw.RequirePermission(h.perm, "ai_agents:update"), h.CreateTrigger)
	ai.PUT("/agents/:id/triggers/:tid", rbacmw.RequirePermission(h.perm, "ai_agents:update"), h.UpdateTrigger)
	ai.DELETE("/agents/:id/triggers/:tid", rbacmw.RequirePermission(h.perm, "ai_agents:update"), h.DeleteTrigger)
	ai.POST("/agents/:id/runs", rbacmw.RequirePermission(h.perm, "ai_agents:execute"), h.ManualRun)
	// API trigger also accepts PAT scope agents:run (checked in middleware/handler).
	ai.POST("/agents/:id/api-runs", h.APIRun)

	ai.GET("/models", rbacmw.RequirePermission(h.perm, "ai_agents:view"), h.ListHarnessModels)
	ai.GET("/agents-defs", rbacmw.RequirePermission(h.perm, "ai_agents:view"), h.ListHarnessAgentDefs)

	ai.GET("/runs", rbacmw.RequirePermission(h.perm, "ai_runs:view"), h.ListRuns)
	ai.GET("/runs/:id", rbacmw.RequirePermission(h.perm, "ai_runs:view"), h.GetRun)
	ai.GET("/runs/:id/artifact", rbacmw.RequirePermission(h.perm, "ai_runs:view"), h.DownloadRunArtifact)
	ai.POST("/runs/:id/cancel", rbacmw.RequirePermission(h.perm, "ai_agents:execute"), h.CancelRun)

	ai.GET("/providers", rbacmw.RequirePermission(h.perm, "ai_providers:view"), h.ListProviders)
	ai.POST("/providers", rbacmw.RequirePermission(h.perm, "ai_providers:create"), h.CreateProvider)
	ai.GET("/providers/:id", rbacmw.RequirePermission(h.perm, "ai_providers:view"), h.GetProvider)
	ai.PUT("/providers/:id", rbacmw.RequirePermission(h.perm, "ai_providers:update"), h.UpdateProvider)
	ai.DELETE("/providers/:id", rbacmw.RequirePermission(h.perm, "ai_providers:delete"), h.DeleteProvider)

	ai.GET("/providers/:id/models", rbacmw.RequirePermission(h.perm, "ai_providers:view"), h.ListModels)
	ai.POST("/providers/:id/models", rbacmw.RequirePermission(h.perm, "ai_providers:create"), h.CreateModel)
	ai.GET("/providers/:id/models/:mid", rbacmw.RequirePermission(h.perm, "ai_providers:view"), h.GetModel)
	ai.PUT("/providers/:id/models/:mid", rbacmw.RequirePermission(h.perm, "ai_providers:update"), h.UpdateModel)
	ai.DELETE("/providers/:id/models/:mid", rbacmw.RequirePermission(h.perm, "ai_providers:delete"), h.DeleteModel)

	if h.chat != nil {
		ai.GET("/chat/sessions", h.chat.ListSessions)
		ai.POST("/chat/sessions", h.chat.CreateSession)
		ai.PUT("/chat/sessions/:id", h.chat.UpdateSession)
		ai.DELETE("/chat/sessions/:id", h.chat.DeleteSession)
		ai.GET("/chat/sessions/:id/messages", h.chat.ListMessages)
		ai.POST("/chat/sessions/:id/messages", h.chat.CreateMessage)
		ai.GET("/chat/models", h.chat.ListAvailableModels)
		completionsAuth := authMW
		if h.chatCompletionsAuth != nil {
			completionsAuth = h.chatCompletionsAuth
		}
		chatCompletions := rg.Group("/ai")
		chatCompletions.POST("/chat/completions", completionsAuth, h.chat.ChatCompletions)
	}

	skills := rg.Group("/skills", authMW)
	skills.GET("", rbacmw.RequirePermission(h.perm, "ai_skills:view"), h.ListSkills)
	skills.POST("", rbacmw.RequirePermission(h.perm, "ai_skills:create"), h.CreateSkill)
	skills.GET("/:id", rbacmw.RequirePermission(h.perm, "ai_skills:view"), h.GetSkill)
	skills.PUT("/:id", rbacmw.RequirePermission(h.perm, "ai_skills:update"), h.OverwriteSkill)
	skills.DELETE("/:id", rbacmw.RequirePermission(h.perm, "ai_skills:delete"), h.DeleteSkill)
	skills.GET("/:id/package", h.DownloadSkill)
	skills.GET("/:id/files", rbacmw.RequirePermission(h.perm, "ai_skills:view"), h.ListSkillFiles)
	skills.GET("/:id/files/content", rbacmw.RequirePermission(h.perm, "ai_skills:view"), h.ReadSkillFile)
	skills.PUT("/:id/files/content", rbacmw.RequirePermission(h.perm, "ai_skills:update"), h.WriteSkillFile)
	skills.POST("/:id/files", rbacmw.RequirePermission(h.perm, "ai_skills:update"), h.CreateSkillEntry)
	skills.DELETE("/:id/files", rbacmw.RequirePermission(h.perm, "ai_skills:update"), h.DeleteSkillEntry)
	skills.POST("/:id/files/rename", rbacmw.RequirePermission(h.perm, "ai_skills:update"), h.RenameSkillEntry)
}

// agentActor resolves the requesting user + data scope for agent-scoped endpoints.
func (h *Handler) agentActor(c *gin.Context) (service.AgentActor, bool) {
	scope, err := h.perm.ResolveDataScope(authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return service.AgentActor{}, false
	}
	return service.AgentActor{UserID: authmiddleware.GetUserID(c), DataScope: scope}, true
}

func (h *Handler) ListAgents(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	items, total, err := h.agents.ListAgents(page, pageSize, actor)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Paginated(c, items, total, page, pageSize)
}

func (h *Handler) CreateAgent(c *gin.Context) {
	var input service.AgentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	item, err := h.agents.CreateAgent(authmiddleware.GetUserID(c), input)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Created(c, item)
}

func (h *Handler) GetAgent(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	item, err := h.agents.GetAgent(uint(id), actor)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) UpdateAgent(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var input service.AgentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	item, err := h.agents.UpdateAgent(uint(id), actor, input)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) DeleteAgent(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	if err := h.agents.DeleteAgent(uint(id), actor); err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, gin.H{"deleted": true})
}

func (h *Handler) ListTriggers(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	items, err := h.agents.ListTriggers(uint(id), actor)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *Handler) CreateTrigger(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var input service.TriggerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	item, err := h.agents.CreateTrigger(uint(id), actor, input)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Created(c, item)
}

func (h *Handler) UpdateTrigger(c *gin.Context) {
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)
	var input service.TriggerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	item, err := h.agents.UpdateTrigger(uint(tid), actor, input)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) DeleteTrigger(c *gin.Context) {
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	if err := h.agents.DeleteTrigger(uint(tid), actor); err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, gin.H{"deleted": true})
}

func (h *Handler) ManualRun(c *gin.Context) {
	if !h.agents.HarnessEnabled() {
		pkg.Error(c, http.StatusServiceUnavailable, "会话底座未启用，无法执行智能体运行")
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var input struct {
		UserPrompt string `json:"user_prompt"`
	}
	_ = c.ShouldBindJSON(&input)
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	run, err := h.agents.ManualRun(uint(id), actor, input.UserPrompt)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: run})
}

func (h *Handler) APIRun(c *gin.Context) {
	// JWT needs ai_agents:execute; PAT needs agents:run scope.
	if authmiddleware.IsPAT(c) {
		if err := authmiddleware.RequirePATScope(c, "agents:run"); err != nil {
			pkg.Error(c, http.StatusForbidden, "token scope insufficient")
			return
		}
	} else if err := h.perm.CheckAccess(authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), "ai_agents:execute"); err != nil {
		pkg.Error(c, http.StatusForbidden, "forbidden")
		return
	}
	if !h.agents.HarnessEnabled() {
		pkg.Error(c, http.StatusServiceUnavailable, "会话底座未启用，无法执行智能体运行")
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var input struct {
		UserPrompt string `json:"user_prompt"`
	}
	_ = c.ShouldBindJSON(&input)
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	run, err := h.agents.APIRun(uint(id), actor, input.UserPrompt)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: run})
}

// ListHarnessModels passes the harness model catalog through (agent config
// page model selector). The catalog is anchored at the workspace root, whose
// opencode.json carries the platform BYOK providers; entries mapping to a
// bedrock provider model are enriched with its reasoning options. 503 when
// the session backend is not enabled or the managed serve is unavailable.
func (h *Handler) ListHarnessModels(c *gin.Context) {
	if h.harness == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "会话底座未启用")
		return
	}
	models, err := h.harness.ListModels(c.Request.Context(), h.agents.WorkspaceRoot())
	if err != nil {
		writeHarnessErr(c, err)
		return
	}
	efforts := map[string][]model.ReasoningEffortOption{}
	if h.providers != nil {
		if providers, perr := h.providers.ListEnabledProvidersWithModels(); perr == nil {
			for _, p := range providers {
				for _, m := range p.Models {
					if len(m.ReasoningEfforts) == 0 {
						continue
					}
					efforts[service.HarnessProviderKey(p.ID)+"|"+m.ModelID] = m.ReasoningEfforts
				}
			}
		}
	}
	items := make([]harnessModelResponse, 0, len(models))
	for _, m := range models {
		if !service.IsBedrockHarnessProvider(m.ProviderID) {
			continue
		}
		item := harnessModelResponse{
			ID: m.ID, ProviderID: m.ProviderID, Name: m.Name, Family: m.Family,
		}
		if opts, ok := efforts[m.ProviderID+"|"+m.ID]; ok {
			item.ReasoningEfforts = opts
		}
		items = append(items, item)
	}
	pkg.Success(c, items)
}

// harnessModelResponse mirrors provider.ModelInfo plus the reasoning options
// of the underlying ai model (bedrock BYOK entries only).
type harnessModelResponse struct {
	ID               string                        `json:"id"`
	ProviderID       string                        `json:"providerID"`
	Name             string                        `json:"name,omitempty"`
	Family           string                        `json:"family,omitempty"`
	ReasoningEfforts []model.ReasoningEffortOption `json:"reasoning_efforts,omitempty"`
}

// ListHarnessAgentDefs passes the harness agent-definition catalog through:
// built-in definitions plus, with ?agent_id=, the compiled bedrock-* artifacts
// of that agent's workspace.
func (h *Handler) ListHarnessAgentDefs(c *gin.Context) {
	if h.harness == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "会话底座未启用")
		return
	}
	directory := ""
	if raw := c.Query("agent_id"); raw != "" {
		agentID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || agentID == 0 {
			pkg.Error(c, http.StatusBadRequest, "无效 agent_id")
			return
		}
		dir, err := h.agents.AgentWorkspaceDir(uint(agentID))
		if err != nil {
			writeErr(c, err)
			return
		}
		directory = dir
	}
	defs, err := h.harness.ListAgents(c.Request.Context(), directory)
	if err != nil {
		writeHarnessErr(c, err)
		return
	}
	pkg.Success(c, defs)
}

// writeHarnessErr maps harness passthrough failures: an unavailable backend
// is a 503, anything else a 502.
func writeHarnessErr(c *gin.Context, err error) {
	if errors.Is(err, provider.ErrUnavailable) {
		pkg.Error(c, http.StatusServiceUnavailable, "会话底座不可用: "+err.Error())
		return
	}
	pkg.Error(c, http.StatusBadGateway, err.Error())
}

func (h *Handler) ListRuns(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	var agentID uint
	if raw := c.Query("agent_id"); raw != "" {
		v, _ := strconv.ParseUint(raw, 10, 64)
		agentID = uint(v)
	}
	projectID, err := parseOptionalUintQuery(c, "project_id")
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 project_id")
		return
	}
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	items, total, err := h.agents.ListRuns(page, pageSize, agentID, c.Query("status"), projectID, actor)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Paginated(c, items, total, page, pageSize)
}

func (h *Handler) GetRun(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	run, err := h.agents.RequireRunAccess(uint(id), actor)
	if err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, run)
}

func (h *Handler) DownloadRunArtifact(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	path, filename, err := h.agents.ArtifactPath(uint(id), actor)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			pkg.Error(c, http.StatusNotFound, "资源不存在")
			return
		}
		if errors.Is(err, service.ErrAgentForbidden) {
			pkg.Error(c, http.StatusForbidden, err.Error())
			return
		}
		pkg.Error(c, http.StatusNotFound, err.Error())
		return
	}
	c.FileAttachment(path, filename)
}

func (h *Handler) CancelRun(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	actor, ok := h.agentActor(c)
	if !ok {
		return
	}
	if _, err := h.agents.RequireRunAccess(uint(id), actor); err != nil {
		writeErr(c, err)
		return
	}
	if err := h.agents.CancelRun(uint(id)); err != nil {
		writeErr(c, err)
		return
	}
	pkg.Success(c, gin.H{"cancelled": true})
}

func (h *Handler) ListSkills(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	scope, err := h.perm.ResolveDataScope(authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	items, total, err := h.skills.List(page, pageSize, authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), scope)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Paginated(c, items, total, page, pageSize)
}

func (h *Handler) GetSkill(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	scope, err := h.perm.ResolveDataScope(authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	item, err := h.skills.Get(uint(id), authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), scope)
	if err != nil {
		writeSkillErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) CreateSkill(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "缺少 file")
		return
	}
	defer file.Close()
	item, err := h.skills.Create(service.SkillUploadInput{
		Name: c.PostForm("name"), Description: c.PostForm("description"),
		Visibility: defaultStr(c.PostForm("visibility"), "private"),
		Filename:   header.Filename, ContentType: header.Header.Get("Content-Type"),
		Size: header.Size, Source: file, UserID: authmiddleware.GetUserID(c),
		IsSuperAdmin: authmiddleware.IsSuperAdmin(c),
	})
	if err != nil {
		writeSkillErr(c, err)
		return
	}
	pkg.Created(c, item)
}

func (h *Handler) OverwriteSkill(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "缺少 file")
		return
	}
	defer file.Close()
	item, err := h.skills.Overwrite(uint(id), service.SkillUploadInput{
		Name: c.PostForm("name"), Description: c.PostForm("description"),
		Visibility: c.PostForm("visibility"),
		Filename:   header.Filename, ContentType: header.Header.Get("Content-Type"),
		Size: header.Size, Source: file, UserID: authmiddleware.GetUserID(c),
		IsSuperAdmin: authmiddleware.IsSuperAdmin(c),
	})
	if err != nil {
		writeSkillErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) DeleteSkill(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.skills.Delete(uint(id), authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c)); err != nil {
		writeSkillErr(c, err)
		return
	}
	pkg.Success(c, gin.H{"deleted": true})
}

func (h *Handler) DownloadSkill(c *gin.Context) {
	if authmiddleware.IsPAT(c) {
		if err := authmiddleware.RequirePATScope(c, "skills:read"); err != nil {
			pkg.Error(c, http.StatusForbidden, "token scope insufficient")
			return
		}
	} else if err := h.perm.CheckAccess(authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), "ai_skills:download"); err != nil {
		pkg.Error(c, http.StatusForbidden, "forbidden")
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	scope, err := h.perm.ResolveDataScope(authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	skill, rc, filename, err := h.skills.OpenPackage(uint(id), authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), scope)
	if err != nil {
		writeSkillErr(c, err)
		return
	}
	defer rc.Close()
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("X-Skill-Digest", skill.PackageDigest)
	c.DataFromReader(http.StatusOK, skill.SizeBytes, "application/zip", rc, nil)
}

func (h *Handler) ListSkillFiles(c *gin.Context) {
	id, scope, ok := h.skillScope(c)
	if !ok {
		return
	}
	items, err := h.skills.ListFiles(id, authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), scope)
	if err != nil {
		writeSkillErr(c, err)
		return
	}
	pkg.Success(c, items)
}

func (h *Handler) ReadSkillFile(c *gin.Context) {
	id, scope, ok := h.skillScope(c)
	if !ok {
		return
	}
	path := strings.TrimSpace(c.Query("path"))
	if path == "" {
		pkg.Error(c, http.StatusBadRequest, "缺少 path")
		return
	}
	item, err := h.skills.ReadFile(id, authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), scope, path)
	if err != nil {
		writeSkillErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) WriteSkillFile(c *gin.Context) {
	id, scope, ok := h.skillScope(c)
	if !ok {
		return
	}
	var input service.SkillWriteFileInput
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.Path) == "" {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	item, err := h.skills.WriteFile(id, authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), scope, input)
	if err != nil {
		writeSkillErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) CreateSkillEntry(c *gin.Context) {
	id, scope, ok := h.skillScope(c)
	if !ok {
		return
	}
	var input service.SkillCreateEntryInput
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.Path) == "" {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	item, err := h.skills.CreateEntry(id, authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), scope, input)
	if err != nil {
		writeSkillErr(c, err)
		return
	}
	pkg.Created(c, item)
}

func (h *Handler) DeleteSkillEntry(c *gin.Context) {
	id, scope, ok := h.skillScope(c)
	if !ok {
		return
	}
	path := strings.TrimSpace(c.Query("path"))
	if path == "" {
		pkg.Error(c, http.StatusBadRequest, "缺少 path")
		return
	}
	if err := h.skills.DeleteEntry(id, authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), scope, path); err != nil {
		writeSkillErr(c, err)
		return
	}
	pkg.Success(c, gin.H{"deleted": true})
}

func (h *Handler) RenameSkillEntry(c *gin.Context) {
	id, scope, ok := h.skillScope(c)
	if !ok {
		return
	}
	var input service.SkillRenameInput
	if err := c.ShouldBindJSON(&input); err != nil ||
		strings.TrimSpace(input.FromPath) == "" || strings.TrimSpace(input.ToPath) == "" {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	item, err := h.skills.RenameEntry(id, authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), scope, input)
	if err != nil {
		writeSkillErr(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *Handler) skillScope(c *gin.Context) (uint, string, bool) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	scope, err := h.perm.ResolveDataScope(authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return 0, "", false
	}
	return uint(id), scope, true
}

func parseOptionalUintQuery(c *gin.Context, key string) (*uint, error) {
	v := c.Query(key)
	if v == "" {
		return nil, nil
	}
	id, err := strconv.ParseUint(v, 10, 64)
	if err != nil || id == 0 {
		return nil, fmt.Errorf("invalid %s", key)
	}
	u := uint(id)
	return &u, nil
}

func writeErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		pkg.Error(c, http.StatusNotFound, "资源不存在")
	case errors.Is(err, service.ErrAgentForbidden):
		pkg.Error(c, http.StatusForbidden, err.Error())
	default:
		pkg.Error(c, http.StatusBadRequest, err.Error())
	}
}

func writeSkillErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrMissingSkillMD):
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, service.ErrSkillForbidden), errors.Is(err, service.ErrSkillReadOnly):
		pkg.Error(c, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrSkillNotFound), errors.Is(err, service.ErrSkillFileNotFound):
		pkg.Error(c, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrSkillFileExists):
		pkg.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrSkillPathInvalid),
		errors.Is(err, service.ErrSkillFileTooLarge),
		errors.Is(err, service.ErrSkillBinaryFile):
		pkg.Error(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, storageservice.ErrTooLarge):
		pkg.Error(c, http.StatusRequestEntityTooLarge, err.Error())
	default:
		writeErr(c, err)
	}
}

func defaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
