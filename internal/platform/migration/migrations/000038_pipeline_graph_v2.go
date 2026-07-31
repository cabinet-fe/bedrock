package migrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000038_pipeline_graph_v2", upPipelineGraphV2)
}

// upPipelineGraphV2 adds typed-node stage columns + run env-override ciphers,
// and backfills legacy buildJob-only graphs with start/end wrapper nodes.
func upPipelineGraphV2(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	stage := &pipelineStageRunV2MigrationModel{}
	for _, col := range []string{"NodeType", "ScriptJobID", "ScriptRunID", "AgentID", "AgentRunID"} {
		if !db.Migrator().HasColumn(stage, col) {
			if err := db.Migrator().AddColumn(stage, col); err != nil {
				return err
			}
		}
	}
	buildRun := &buildRunEnvOverridesMigrationModel{}
	if !db.Migrator().HasColumn(buildRun, "EnvOverridesCipher") {
		if err := db.Migrator().AddColumn(buildRun, "EnvOverridesCipher"); err != nil {
			return err
		}
	}
	scriptRun := &scriptRunEnvOverridesMigrationModel{}
	if !db.Migrator().HasColumn(scriptRun, "EnvOverridesCipher") {
		if err := db.Migrator().AddColumn(scriptRun, "EnvOverridesCipher"); err != nil {
			return err
		}
	}

	// Backfill graph_json: wrap legacy graphs with start/end nodes.
	var rows []struct {
		ID        uint
		GraphJSON string `gorm:"column:graph_json"`
	}
	if err := db.Table("build_pipelines").Select("id", "graph_json").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		upgraded, err := UpgradePipelineGraphJSON(row.GraphJSON)
		if err != nil || upgraded == row.GraphJSON {
			continue
		}
		if err := db.Table("build_pipelines").Where("id = ?", row.ID).
			Update("graph_json", upgraded).Error; err != nil {
			return err
		}
	}
	return nil
}

type pipelineStageRunV2MigrationModel struct {
	ID          uint   `gorm:"primaryKey"`
	NodeType    string `gorm:"size:20;not null;default:buildJob"`
	ScriptJobID uint   `gorm:"not null;default:0"`
	ScriptRunID *uint  `gorm:"index"`
	AgentID     uint   `gorm:"not null;default:0"`
	AgentRunID  *uint  `gorm:"index"`
}

func (pipelineStageRunV2MigrationModel) TableName() string { return "pipeline_stage_runs" }

type buildRunEnvOverridesMigrationModel struct {
	ID                 uint   `gorm:"primaryKey"`
	EnvOverridesCipher string `gorm:"type:text"`
}

func (buildRunEnvOverridesMigrationModel) TableName() string { return "build_runs" }

type scriptRunEnvOverridesMigrationModel struct {
	ID                 uint   `gorm:"primaryKey"`
	EnvOverridesCipher string `gorm:"type:text"`
}

func (scriptRunEnvOverridesMigrationModel) TableName() string { return "script_runs" }

// UpgradePipelineGraphJSON wraps a legacy (buildJob-only, no start node) graph
// with start/end nodes: start → every indegree-0 node, every outdegree-0 node → end.
// Already-v2 graphs (any node typed "start") are returned unchanged.
func UpgradePipelineGraphJSON(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = `{"nodes":[],"edges":[]}`
	}
	var g map[string]any
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return "", fmt.Errorf("graph_json 无效: %w", err)
	}
	nodes := jsonArray(g["nodes"])
	edges := jsonArray(g["edges"])

	for _, n := range nodes {
		if m, ok := n.(map[string]any); ok {
			if t, _ := m["type"].(string); t == "start" {
				return raw, nil // already v2
			}
		}
	}

	indeg := map[string]int{}
	outdeg := map[string]int{}
	nodeID := func(n any) string {
		if m, ok := n.(map[string]any); ok {
			id, _ := m["id"].(string)
			return id
		}
		return ""
	}
	for _, n := range nodes {
		indeg[nodeID(n)] = 0
		outdeg[nodeID(n)] = 0
	}
	for _, e := range edges {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		src, _ := m["source"].(string)
		tgt, _ := m["target"].(string)
		if _, ok := indeg[tgt]; ok {
			indeg[tgt]++
			outdeg[src]++
		}
	}

	startID := freeNodeID(indeg, "__start__")
	endID := freeNodeID(indeg, "__end__")
	startX, endX, midY := wrapperPositions(nodes)

	startNode := map[string]any{
		"id": startID, "type": "start",
		"position": map[string]any{"x": startX, "y": midY},
		"data":     map[string]any{"label": "开始"},
	}
	endNode := map[string]any{
		"id": endID, "type": "end",
		"position": map[string]any{"x": endX, "y": midY},
		"data":     map[string]any{"label": "结束"},
	}

	out := make([]any, 0, len(nodes)+2)
	out = append(out, startNode)
	out = append(out, nodes...)
	out = append(out, endNode)
	for _, n := range nodes {
		id := nodeID(n)
		if id == "" {
			continue
		}
		if indeg[id] == 0 {
			edges = append(edges, map[string]any{
				"id": "e-" + startID + "-" + id, "source": startID, "target": id,
				"data": map[string]any{"condition": "on_success"},
			})
		}
		if outdeg[id] == 0 {
			edges = append(edges, map[string]any{
				"id": "e-" + id + "-" + endID, "source": id, "target": endID,
				"data": map[string]any{"condition": "on_success"},
			})
		}
	}
	g["nodes"] = out
	g["edges"] = edges
	b, err := json.Marshal(g)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func jsonArray(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return []any{}
}

func freeNodeID(existing map[string]int, base string) string {
	if _, ok := existing[base]; !ok {
		return base
	}
	for i := 2; ; i++ {
		id := fmt.Sprintf("%s%d", base, i)
		if _, ok := existing[id]; !ok {
			return id
		}
	}
}

// wrapperPositions computes x for start (left of all) / end (right of all) and
// the average y, so injected nodes don't overlap existing ones.
func wrapperPositions(nodes []any) (startX, endX, midY float64) {
	if len(nodes) == 0 {
		return 40, 560, 120
	}
	minX, maxX := 0.0, 0.0
	sumY := 0.0
	first := true
	for _, n := range nodes {
		m, ok := n.(map[string]any)
		if !ok {
			continue
		}
		pos, ok := m["position"].(map[string]any)
		if !ok {
			continue
		}
		x, _ := pos["x"].(float64)
		y, _ := pos["y"].(float64)
		if first || x < minX {
			minX = x
		}
		if first || x > maxX {
			maxX = x
		}
		first = false
		sumY += y
	}
	return minX - 240, maxX + 260, sumY / float64(len(nodes))
}
