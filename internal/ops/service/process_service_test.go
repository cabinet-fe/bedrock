package service

import (
	"testing"

	"bedrock/internal/ops/model"
)

func TestSortProcessesProTableSpec(t *testing.T) {
	items := []model.ProcessInfo{
		{PID: 1, Name: "beta", CPUPercent: 10, MemoryBytes: 200},
		{PID: 2, Name: "alpha", CPUPercent: 30, MemoryBytes: 100},
		{PID: 3, Name: "gamma", CPUPercent: 20, MemoryBytes: 300},
	}

	sortProcesses(items, "cpu_percent@desc")
	if items[0].PID != 2 || items[1].PID != 3 || items[2].PID != 1 {
		t.Fatalf("cpu_percent@desc order = %+v", pids(items))
	}

	sortProcesses(items, "memory_bytes@asc")
	if items[0].PID != 2 || items[1].PID != 1 || items[2].PID != 3 {
		t.Fatalf("memory_bytes@asc order = %+v", pids(items))
	}

	sortProcesses(items, "name@asc")
	if items[0].Name != "alpha" || items[1].Name != "beta" || items[2].Name != "gamma" {
		t.Fatalf("name@asc order = %+v", names(items))
	}

	sortProcesses(items, "")
	if items[0].PID != 1 || items[1].PID != 2 || items[2].PID != 3 {
		t.Fatalf("empty sort should fall back to PID: %+v", pids(items))
	}
}

func pids(items []model.ProcessInfo) []int32 {
	out := make([]int32, len(items))
	for i, item := range items {
		out[i] = item.PID
	}
	return out
}

func names(items []model.ProcessInfo) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Name
	}
	return out
}
