package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	gomem "github.com/shirou/gopsutil/v4/mem"
	"gorm.io/gorm"

	"bedrock/internal/dashboard/model"
	"bedrock/internal/dashboard/repository"
	"bedrock/internal/rbac"
	"bedrock/internal/ws"
)

const (
	CardBuildSummary        = "build_summary"
	CardAgentRunSummary     = "agent_run_summary"
	CardSystemInfo          = "system_info"
	CardSystemStatus        = "system_status"
	CardScriptRunSummary    = "script_run_summary"
	CardPipelineRunSummary  = "pipeline_run_summary"
	CardCICDTaskOverview    = "cicd_task_overview"
	CardMyProjects          = "my_projects"

	gridColumns = 12
	minCardSize = 2
)

var ErrUnauthorizedCard = errors.New("仪表盘包含无权限卡片")

// defaultCardGeometry is the 12-column layout used for new users and legacy
// cards that omit x/y/w/h.
var defaultCardGeometry = map[string]struct{ X, Y, W, H int }{
	CardBuildSummary:       {0, 0, 6, 4},
	CardAgentRunSummary:    {6, 0, 6, 4},
	CardSystemInfo:         {0, 4, 6, 3},
	CardSystemStatus:       {6, 4, 6, 3},
	CardScriptRunSummary:   {0, 7, 6, 4},
	CardPipelineRunSummary: {6, 7, 6, 4},
	CardCICDTaskOverview:   {0, 11, 6, 3},
	CardMyProjects:         {6, 11, 6, 3},
}

type DashboardService struct {
	repo      *repository.DashboardRepository
	version   string
	startTime time.Time
	diskPaths []string
}

func NewDashboardService(repo *repository.DashboardRepository, version string, startTime time.Time, diskPaths []string) *DashboardService {
	paths := make([]string, 0, len(diskPaths))
	for _, path := range diskPaths {
		if path != "" {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		paths = append(paths, ".")
	}
	return &DashboardService{repo: repo, version: version, startTime: startTime.UTC(), diskPaths: paths}
}

func (s *DashboardService) GetLayout(userID uint, isSuperAdmin bool, permissions []string) (*model.LayoutResponse, error) {
	allowed := allowedCards(isSuperAdmin, permissions)
	layout, err := s.repo.FindLayoutByUserID(userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &model.LayoutResponse{Cards: defaultLayout(allowed)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find dashboard layout: %w", err)
	}

	cards, err := decodeCards(layout.CardsJSON)
	if err != nil {
		// A corrupted old preference should never make the dashboard unusable.
		return &model.LayoutResponse{Cards: defaultLayout(allowed)}, nil
	}
	return &model.LayoutResponse{Cards: normalizeCards(cards, allowed)}, nil
}

func (s *DashboardService) PutLayout(userID uint, isSuperAdmin bool, permissions []string, cards []model.CardLayout) (*model.LayoutResponse, error) {
	allowed := allowedCards(isSuperAdmin, permissions)
	if err := validateCards(cards, allowed); err != nil {
		return nil, err
	}
	normalized := normalizeCards(cards, allowed)
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode dashboard layout: %w", err)
	}
	layout, err := s.repo.FindLayoutByUserID(userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := s.repo.CreateLayout(&model.Layout{UserID: userID, CardsJSON: string(raw)}); err != nil {
			return nil, fmt.Errorf("create dashboard layout: %w", err)
		}
		return &model.LayoutResponse{Cards: normalized}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find dashboard layout: %w", err)
	}
	layout.CardsJSON = string(raw)
	if err := s.repo.UpdateLayout(layout); err != nil {
		return nil, fmt.Errorf("update dashboard layout: %w", err)
	}
	return &model.LayoutResponse{Cards: normalized}, nil
}

func (s *DashboardService) BuildSummary() (*model.BuildSummary, error) {
	running, err := s.repo.CountRunsByStatus("running")
	if err != nil {
		return nil, err
	}
	queued, err := s.repo.CountRunsByStatus("queued")
	if err != nil {
		return nil, err
	}
	total, success, err := s.repo.CountFinishedRuns()
	if err != nil {
		return nil, err
	}
	recent, err := s.repo.ListRecentRuns(8)
	if err != nil {
		return nil, err
	}
	rate := float64(0)
	if total > 0 {
		rate = float64(success) * 100 / float64(total)
	}
	return &model.BuildSummary{Running: running, Queued: queued, SuccessRate: rate, Recent: recent}, nil
}

func (s *DashboardService) AgentRunSummary() (*model.AgentRunSummary, error) {
	running, err := s.repo.CountAgentRunsByStatus("running")
	if err != nil {
		return nil, err
	}
	queued, err := s.repo.CountAgentRunsByStatuses([]string{"queued", "pending"})
	if err != nil {
		return nil, err
	}
	total, success, err := s.repo.CountFinishedAgentRuns()
	if err != nil {
		return nil, err
	}
	recent, err := s.repo.ListRecentAgentRuns(8)
	if err != nil {
		return nil, err
	}
	rate := float64(0)
	if total > 0 {
		rate = float64(success) * 100 / float64(total)
	}
	return &model.AgentRunSummary{Running: running, Queued: queued, SuccessRate: rate, Recent: recent}, nil
}

func (s *DashboardService) ScriptRunSummary() (*model.ScriptRunSummary, error) {
	running, err := s.repo.CountScriptRunsByStatus("running")
	if err != nil {
		return nil, err
	}
	queued, err := s.repo.CountScriptRunsByStatus("queued")
	if err != nil {
		return nil, err
	}
	total, success, err := s.repo.CountFinishedScriptRuns()
	if err != nil {
		return nil, err
	}
	recent, err := s.repo.ListRecentScriptRuns(8)
	if err != nil {
		return nil, err
	}
	rate := float64(0)
	if total > 0 {
		rate = float64(success) * 100 / float64(total)
	}
	return &model.ScriptRunSummary{Running: running, Queued: queued, SuccessRate: rate, Recent: recent}, nil
}

func (s *DashboardService) PipelineRunSummary() (*model.PipelineRunSummary, error) {
	running, err := s.repo.CountPipelineRunsByStatus("running")
	if err != nil {
		return nil, err
	}
	queued, err := s.repo.CountPipelineRunsByStatus("queued")
	if err != nil {
		return nil, err
	}
	total, success, err := s.repo.CountFinishedPipelineRuns()
	if err != nil {
		return nil, err
	}
	recent, err := s.repo.ListRecentPipelineRuns(8)
	if err != nil {
		return nil, err
	}
	rate := float64(0)
	if total > 0 {
		rate = float64(success) * 100 / float64(total)
	}
	return &model.PipelineRunSummary{Running: running, Queued: queued, SuccessRate: rate, Recent: recent}, nil
}

func (s *DashboardService) TaskOverview(isSuperAdmin bool, permissions []string) (*model.TaskOverview, error) {
	out := &model.TaskOverview{}
	if isSuperAdmin || hasPermission(permissions, "cicd_build_jobs:view") {
		n, err := s.repo.CountBuildJobs()
		if err != nil {
			return nil, err
		}
		out.BuildJobs = &n
	}
	if isSuperAdmin || hasPermission(permissions, "cicd_script_jobs:view") {
		n, err := s.repo.CountScriptJobs()
		if err != nil {
			return nil, err
		}
		out.ScriptJobs = &n
	}
	if isSuperAdmin || hasPermission(permissions, "cicd_pipelines:view") {
		n, err := s.repo.CountPipelines()
		if err != nil {
			return nil, err
		}
		out.Pipelines = &n
	}
	return out, nil
}

func (s *DashboardService) MyProjects(userID uint) ([]model.MyProject, error) {
	return s.repo.ListMyProjects(userID, 10)
}

func (s *DashboardService) SystemInfo() (*model.SystemInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}
	return &model.SystemInfo{
		Version: s.version, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Runtime: runtime.Version(), Hostname: hostname, StartTime: s.startTime,
	}, nil
}

func (s *DashboardService) SystemStatus() (*model.SystemStatus, error) {
	result := &model.SystemStatus{Health: "ok", CollectedAt: time.Now().UTC()}
	if samples, err := cpu.Percent(0, false); err == nil && len(samples) > 0 {
		result.CPUUsagePercent = roundSingleDecimal(samples[0])
	}
	if vm, err := gomem.VirtualMemory(); err == nil {
		result.MemoryUsedBytes = vm.Used
		result.MemoryTotalBytes = vm.Total
		result.MemoryUsagePercent = roundSingleDecimal(vm.UsedPercent)
	}
	// Host disk: capacity of the partition that holds configured data directories.
	if usage, err := disk.Usage(s.diskPaths[0]); err == nil {
		result.DiskUsedBytes = usage.Used
		result.DiskTotalBytes = usage.Total
		result.DiskFreeBytes = usage.Free
		result.DiskUsagePercent = roundSingleDecimal(usage.UsedPercent)
	} else {
		result.Health = "degraded"
	}
	for _, path := range s.diskPaths {
		used, err := directoryUsedBytes(path)
		if err != nil {
			result.Health = "degraded"
			result.Directories = append(result.Directories, model.DirectoryUsage{Path: path})
			continue
		}
		result.Directories = append(result.Directories, model.DirectoryUsage{
			Path: path, UsedBytes: used,
		})
	}
	return result, nil
}

// directoryUsedBytes sums regular file sizes under root. Missing roots report 0;
// unreadable entries are skipped so one bad path does not fail the whole sample.
func directoryUsedBytes(root string) (uint64, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return uint64(info.Size()), nil
	}
	var total uint64
	err = filepath.WalkDir(root, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		fi, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if fi.Mode().IsRegular() {
			total += uint64(fi.Size())
		}
		return nil
	})
	return total, err
}

// StartStatusBroadcaster 在有订阅者时周期性采集系统状态并广播；无订阅者时跳过采样。
func (s *DashboardService) StartStatusBroadcaster(ctx context.Context, hub *ws.Hub, interval time.Duration) {
	if hub == nil || interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if hub.ChannelSubscriberCount(ws.ChannelDashboardSystemStatus) == 0 {
					continue
				}
				status, err := s.SystemStatus()
				if err != nil {
					continue
				}
				payload, err := json.Marshal(map[string]interface{}{
					"type": "system_status",
					"data": status,
				})
				if err != nil {
					continue
				}
				hub.BroadcastToChannel(ws.ChannelDashboardSystemStatus, payload)
			}
		}
	}()
}

func allowedCards(isSuperAdmin bool, permissions []string) map[string]struct{} {
	allowed := map[string]struct{}{}
	if isSuperAdmin || hasPermission(permissions, "cicd_build_runs:view") {
		allowed[CardBuildSummary] = struct{}{}
	}
	if isSuperAdmin || hasPermission(permissions, "ai_runs:view") {
		allowed[CardAgentRunSummary] = struct{}{}
	}
	if isSuperAdmin || hasPermission(permissions, "dashboard:system_info") {
		allowed[CardSystemInfo] = struct{}{}
	}
	if isSuperAdmin || hasPermission(permissions, "dashboard:system_status") {
		allowed[CardSystemStatus] = struct{}{}
	}
	if isSuperAdmin || hasPermission(permissions, "cicd_script_runs:view") {
		allowed[CardScriptRunSummary] = struct{}{}
	}
	if isSuperAdmin || hasPermission(permissions, "cicd_pipeline_runs:view") {
		allowed[CardPipelineRunSummary] = struct{}{}
	}
	if isSuperAdmin || hasPermission(permissions, "dashboard:view") {
		allowed[CardCICDTaskOverview] = struct{}{}
	}
	if isSuperAdmin || hasPermission(permissions, "project_projects:view") {
		allowed[CardMyProjects] = struct{}{}
	}
	return allowed
}

func hasPermission(codes []string, required string) bool {
	return rbac.HasPermission(rbac.ToSet(codes), required)
}

func defaultLayout(allowed map[string]struct{}) []model.CardLayout {
	all := []string{
		CardBuildSummary, CardAgentRunSummary, CardSystemInfo, CardSystemStatus,
		CardScriptRunSummary, CardPipelineRunSummary, CardCICDTaskOverview, CardMyProjects,
	}
	cards := make([]model.CardLayout, 0, len(all))
	for _, id := range all {
		if _, ok := allowed[id]; !ok {
			continue
		}
		geo := defaultCardGeometry[id]
		cards = append(cards, model.CardLayout{
			ID: id, Visible: true,
			X: geo.X, Y: geo.Y, W: geo.W, H: geo.H,
			Order: geo.Y*gridColumns + geo.X,
		})
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Order != cards[j].Order {
			return cards[i].Order < cards[j].Order
		}
		return cards[i].ID < cards[j].ID
	})
	return cards
}

func hasCardGeometry(card model.CardLayout) bool {
	return card.W >= minCardSize && card.H >= minCardSize
}

func normalizeCards(cards []model.CardLayout, allowed map[string]struct{}) []model.CardLayout {
	byID := map[string]model.CardLayout{}
	for _, card := range cards {
		if _, ok := allowed[card.ID]; ok {
			byID[card.ID] = card
		}
	}
	for _, defaultCard := range defaultLayout(allowed) {
		if _, ok := byID[defaultCard.ID]; !ok {
			byID[defaultCard.ID] = defaultCard
		}
	}
	out := make([]model.CardLayout, 0, len(byID))
	for _, card := range byID {
		out = append(out, card)
	}
	// Preserve legacy order while assigning missing geometry, then normalize
	// order from y*cols+x so GridStack and list editors stay aligned.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	for i := range out {
		if !hasCardGeometry(out[i]) {
			if geo, ok := defaultCardGeometry[out[i].ID]; ok {
				out[i].X, out[i].Y, out[i].W, out[i].H = geo.X, geo.Y, geo.W, geo.H
			}
		}
		out[i].Order = out[i].Y*gridColumns + out[i].X
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func validateCards(cards []model.CardLayout, allowed map[string]struct{}) error {
	seen := map[string]struct{}{}
	for _, card := range cards {
		if _, ok := allowed[card.ID]; !ok {
			return ErrUnauthorizedCard
		}
		if _, duplicate := seen[card.ID]; duplicate {
			return fmt.Errorf("重复卡片: %s", card.ID)
		}
		seen[card.ID] = struct{}{}
		// Legacy payloads omit geometry (all zeros); normalizeCards fills defaults.
		if card.W == 0 && card.H == 0 {
			continue
		}
		if card.W < minCardSize || card.H < minCardSize {
			return fmt.Errorf("卡片尺寸过小: %s (w/h 最小为 %d)", card.ID, minCardSize)
		}
		if card.W > gridColumns {
			return fmt.Errorf("卡片宽度过大: %s (w 最大为 %d)", card.ID, gridColumns)
		}
	}
	return nil
}

func decodeCards(raw string) ([]model.CardLayout, error) {
	var cards []model.CardLayout
	if err := json.Unmarshal([]byte(raw), &cards); err != nil {
		return nil, err
	}
	return cards, nil
}

func roundSingleDecimal(value float64) float64 {
	return float64(int(value*10+0.5)) / 10
}
