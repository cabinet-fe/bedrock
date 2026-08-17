package service

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	gonet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"

	"bedrock/internal/ops/model"
)

var (
	ErrKillSelf          = errors.New("不能终止 Bedrock 自身进程")
	ErrDangerousProcess  = errors.New("不能终止受保护的系统进程")
	dangerousProcessName = map[string]struct{}{
		"init": {}, "systemd": {}, "launchd": {}, "kernel_task": {},
		"kthreadd": {}, "kswapd": {}, "csrss.exe": {}, "wininit.exe": {},
		"winlogon.exe": {}, "services.exe": {}, "lsass.exe": {}, "bedrock": {},
	}
)

type cpuSample struct {
	times cpu.TimesStat
	at    time.Time
}

// ProcessService is the P2 port of the legacy local process collector. It has
// no database dependency; the handler's ops permission gate protects it.
type ProcessService struct {
	mu       sync.Mutex
	cpuCache map[int32]cpuSample
}

func NewProcessService() *ProcessService {
	return &ProcessService{cpuCache: make(map[int32]cpuSample)}
}

func (s *ProcessService) ListProcesses(opts model.ProcessListOptions) ([]model.ProcessInfo, error) {
	processes, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("enumerate processes: %w", err)
	}
	items := make([]model.ProcessInfo, 0, len(processes))
	alive := make(map[int32]struct{}, len(processes))
	for _, proc := range processes {
		item, ok := s.collect(proc)
		if !ok {
			continue
		}
		alive[item.PID] = struct{}{}
		items = append(items, item)
	}
	s.pruneCPUCache(alive)

	portMap := listeningPorts()
	for i := range items {
		items[i].Ports = portMap[items[i].PID]
		if items[i].Ports == nil {
			items[i].Ports = []uint32{}
		}
	}
	items = filterProcesses(items, opts)
	sortProcesses(items, opts.Sort)
	return items, nil
}

func (s *ProcessService) KillProcess(pid int32) (string, error) {
	if pid <= 0 {
		return "", errors.New("无效的进程 PID")
	}
	if int(pid) == os.Getpid() {
		return "", ErrKillSelf
	}
	proc, err := process.NewProcess(pid)
	if err != nil {
		return "", fmt.Errorf("进程不存在: %w", err)
	}
	name, _ := proc.Name()
	if IsDangerousProcess(pid, name) {
		return name, ErrDangerousProcess
	}
	if name == "" {
		name = fmt.Sprintf("pid-%d", pid)
	}
	if err := proc.Terminate(); err != nil {
		return name, fmt.Errorf("终止进程失败: %w", err)
	}
	return name, nil
}

func IsDangerousProcess(pid int32, name string) bool {
	if pid == 1 {
		return true
	}
	_, ok := dangerousProcessName[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func (s *ProcessService) collect(proc *process.Process) (model.ProcessInfo, bool) {
	name, err := proc.Name()
	if err != nil {
		return model.ProcessInfo{}, false
	}
	item := model.ProcessInfo{PID: proc.Pid, Name: name, Ports: []uint32{}}
	if memory, err := proc.MemoryInfo(); err == nil && memory != nil {
		item.MemoryBytes = memory.RSS
	}
	if username, err := proc.Username(); err == nil {
		item.Username = username
	}
	if threads, err := proc.NumThreads(); err == nil {
		item.NumThreads = threads
	}
	if statuses, err := proc.Status(); err == nil && len(statuses) > 0 {
		item.Status = statuses[0]
	}
	if createTime, err := proc.CreateTime(); err == nil {
		item.StartTime = createTime
	}
	if cmdline, err := proc.Cmdline(); err == nil {
		item.Cmdline = cmdline
	}
	item.CPUPercent = s.cpuPercent(proc)
	return item, true
}

func (s *ProcessService) cpuPercent(proc *process.Process) float64 {
	times, err := proc.Times()
	if err != nil || times == nil {
		return 0
	}
	now := time.Now()
	numCPU := max(runtime.NumCPU(), 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.cpuCache[proc.Pid]
	s.cpuCache[proc.Pid] = cpuSample{times: *times, at: now}
	if !ok {
		return 0
	}
	elapsed := now.Sub(previous.at).Seconds()
	if elapsed <= 0 {
		return 0
	}
	used := (times.User - previous.times.User) + (times.System - previous.times.System)
	if used <= 0 {
		return 0
	}
	return float64(int((used/(elapsed*float64(numCPU))*100)*10+0.5)) / 10
}

func (s *ProcessService) pruneCPUCache(alive map[int32]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for pid := range s.cpuCache {
		if _, ok := alive[pid]; !ok {
			delete(s.cpuCache, pid)
		}
	}
}

func listeningPorts() map[int32][]uint32 {
	connections, err := gonet.Connections("inet")
	if err != nil {
		return map[int32][]uint32{}
	}
	result := map[int32][]uint32{}
	seen := map[int32]map[uint32]struct{}{}
	for _, connection := range connections {
		if connection.Status != "LISTEN" || connection.Pid <= 0 || connection.Laddr.Port == 0 {
			continue
		}
		if seen[connection.Pid] == nil {
			seen[connection.Pid] = map[uint32]struct{}{}
		}
		seen[connection.Pid][connection.Laddr.Port] = struct{}{}
	}
	for pid, ports := range seen {
		for port := range ports {
			result[pid] = append(result[pid], port)
		}
		slices.Sort(result[pid])
	}
	return result
}

func filterProcesses(items []model.ProcessInfo, opts model.ProcessListOptions) []model.ProcessInfo {
	keyword := strings.ToLower(strings.TrimSpace(opts.Keyword))
	if keyword == "" && opts.PID == nil && opts.Port == nil {
		return items
	}
	out := make([]model.ProcessInfo, 0, len(items))
	for _, item := range items {
		if opts.PID != nil && item.PID != *opts.PID {
			continue
		}
		if keyword != "" {
			haystack := strings.ToLower(item.Name + " " + item.Cmdline)
			if !strings.Contains(haystack, keyword) {
				continue
			}
		}
		if opts.Port != nil {
			found := slices.Contains(item.Ports, *opts.Port)
			if !found {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

func sortProcesses(items []model.ProcessInfo, sortSpec string) {
	field, ascending := parseProcessSort(sortSpec)
	sort.SliceStable(items, func(i, j int) bool {
		switch field {
		case "cpu_percent":
			if ascending {
				return items[i].CPUPercent < items[j].CPUPercent
			}
			return items[i].CPUPercent > items[j].CPUPercent
		case "memory_bytes":
			if ascending {
				return items[i].MemoryBytes < items[j].MemoryBytes
			}
			return items[i].MemoryBytes > items[j].MemoryBytes
		case "name":
			if ascending {
				return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
			}
			return strings.ToLower(items[i].Name) > strings.ToLower(items[j].Name)
		default:
			return items[i].PID < items[j].PID
		}
	})
}

// parseProcessSort 解析 ProTable 的 field@asc|desc。
func parseProcessSort(sortSpec string) (field string, ascending bool) {
	sortSpec = strings.TrimSpace(sortSpec)
	if sortSpec == "" {
		return "", false
	}
	at := strings.LastIndex(sortSpec, "@")
	if at <= 0 {
		return "", false
	}
	field = strings.ToLower(sortSpec[:at])
	order := strings.ToLower(sortSpec[at+1:])
	switch field {
	case "cpu_percent", "memory_bytes", "name":
		// ok
	default:
		return "", false
	}
	if order != "asc" && order != "desc" {
		return "", false
	}
	return field, order == "asc"
}
