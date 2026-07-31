package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Pipeline node types (graph_json v2).
const (
	PipelineNodeStart     = "start"
	PipelineNodeEnd       = "end"
	PipelineNodeBuildJob  = "buildJob"
	PipelineNodeScriptJob = "scriptJob"
	PipelineNodeAgent     = "agent"
)

// Pipeline edge conditions (edge.data.condition; empty means on_success).
const (
	EdgeOnSuccess = "on_success"
	EdgeOnFailure = "on_failure"
	EdgeAlways    = "always"
)

// PipelineGraph is the persisted VueFlow nodes/edges shape (subset used for validation).
type PipelineGraph struct {
	Nodes []PipelineGraphNode `json:"nodes"`
	Edges []PipelineGraphEdge `json:"edges"`
}

type PipelineGraphNode struct {
	ID       string           `json:"id"`
	Type     string           `json:"type,omitempty"`
	Position json.RawMessage  `json:"position,omitempty"`
	Data     PipelineNodeData `json:"data"`
}

// NodeType normalizes legacy type-less nodes to buildJob.
func (n PipelineGraphNode) NodeType() string {
	if n.Type == "" {
		return PipelineNodeBuildJob
	}
	return n.Type
}

type PipelineNodeData struct {
	BuildJobID  uint                 `json:"build_job_id,omitempty"`
	ScriptJobID uint                 `json:"script_job_id,omitempty"`
	AgentID     uint                 `json:"agent_id,omitempty"`
	Label       string               `json:"label,omitempty"`
	EnvVars     []PipelineNodeEnvVar `json:"env_vars,omitempty"`
}

// PipelineNodeEnvVar is a node-level env override. At rest Value carries an
// "enc:v1:"-prefixed cipher; read APIs project it to {key, has_value}.
type PipelineNodeEnvVar struct {
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"`
	HasValue bool   `json:"has_value,omitempty"`
}

type PipelineGraphEdge struct {
	ID     string           `json:"id"`
	Source string           `json:"source"`
	Target string           `json:"target"`
	Data   PipelineEdgeData `json:"data,omitempty"`
}

type PipelineEdgeData struct {
	Condition string `json:"condition,omitempty"`
}

// Condition normalizes the edge condition (empty = on_success).
func (e PipelineGraphEdge) Condition() string {
	if e.Data.Condition == "" {
		return EdgeOnSuccess
	}
	return e.Data.Condition
}

// PipelineRefChecker reports whether referenced entities exist (nil = skip check).
type PipelineRefChecker struct {
	BuildJobExists  func(id uint) bool
	ScriptJobExists func(id uint) bool
	AgentExists     func(id uint) bool
}

// EdgeConditionMatches reports whether an edge condition fires for a node's
// terminal outcome (success|failed|cancelled|interrupted).
func EdgeConditionMatches(cond, status string) bool {
	switch cond {
	case "", EdgeOnSuccess:
		return status == "success"
	case EdgeOnFailure:
		return status == "failed" || status == "cancelled" || status == "interrupted"
	case EdgeAlways:
		switch status {
		case "success", "failed", "cancelled", "interrupted":
			return true
		}
		return false
	}
	return false
}

// ParsePipelineGraph unmarshals graph_json.
func ParsePipelineGraph(graphJSON string) (*PipelineGraph, error) {
	raw := strings.TrimSpace(graphJSON)
	if raw == "" {
		return &PipelineGraph{}, nil
	}
	var g PipelineGraph
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, fmt.Errorf("graph_json 无效: %w", err)
	}
	return &g, nil
}

// ValidatePipelineDAG checks v2 graph integrity:
// exactly one start (indegree 0), ≥1 end (outdegree 0), typed refs exist,
// every non-start node has an incoming edge, edge conditions valid, acyclic.
func ValidatePipelineDAG(g *PipelineGraph, refs PipelineRefChecker) error {
	if g == nil {
		return errorsNew("graph_json 不能为空")
	}
	if len(g.Nodes) == 0 {
		return errorsNew("流水线至少需要一个节点")
	}

	nodeIDs := make(map[string]struct{}, len(g.Nodes))
	types := make(map[string]string, len(g.Nodes))
	startCount, endCount := 0, 0
	for i, n := range g.Nodes {
		id := strings.TrimSpace(n.ID)
		if id == "" {
			return fmt.Errorf("节点 #%d 缺少 id", i+1)
		}
		if _, dup := nodeIDs[id]; dup {
			return fmt.Errorf("重复节点 id: %s", id)
		}
		nodeIDs[id] = struct{}{}
		typ := n.NodeType()
		types[id] = typ
		switch typ {
		case PipelineNodeStart:
			startCount++
		case PipelineNodeEnd:
			endCount++
		case PipelineNodeBuildJob:
			if n.Data.BuildJobID == 0 {
				return fmt.Errorf("节点 %s 缺少 build_job_id", id)
			}
			if refs.BuildJobExists != nil && !refs.BuildJobExists(n.Data.BuildJobID) {
				return fmt.Errorf("节点 %s 引用的构建任务不存在: %d", id, n.Data.BuildJobID)
			}
		case PipelineNodeScriptJob:
			if n.Data.ScriptJobID == 0 {
				return fmt.Errorf("节点 %s 缺少 script_job_id", id)
			}
			if refs.ScriptJobExists != nil && !refs.ScriptJobExists(n.Data.ScriptJobID) {
				return fmt.Errorf("节点 %s 引用的脚本任务不存在: %d", id, n.Data.ScriptJobID)
			}
		case PipelineNodeAgent:
			if n.Data.AgentID == 0 {
				return fmt.Errorf("节点 %s 缺少 agent_id", id)
			}
			if refs.AgentExists != nil && !refs.AgentExists(n.Data.AgentID) {
				return fmt.Errorf("节点 %s 引用的智能体不存在: %d", id, n.Data.AgentID)
			}
		default:
			return fmt.Errorf("节点 %s 类型无效: %s", id, typ)
		}
		for _, kv := range n.Data.EnvVars {
			if err := validateEnvVarKey(kv.Key); err != nil {
				return fmt.Errorf("节点 %s 变量无效: %w", id, err)
			}
		}
	}
	if startCount != 1 {
		return fmt.Errorf("流水线需要恰好 1 个开始节点，当前 %d 个", startCount)
	}
	if endCount == 0 {
		return errorsNew("流水线至少需要 1 个结束节点")
	}

	// adjacency + indegree for structural checks and Kahn
	succ := make(map[string][]string, len(g.Nodes))
	indeg := make(map[string]int, len(g.Nodes))
	for id := range nodeIDs {
		indeg[id] = 0
	}
	for i, e := range g.Edges {
		src := strings.TrimSpace(e.Source)
		tgt := strings.TrimSpace(e.Target)
		if src == "" || tgt == "" {
			return fmt.Errorf("边 #%d 缺少 source/target", i+1)
		}
		if _, ok := nodeIDs[src]; !ok {
			return fmt.Errorf("边引用未知 source: %s", src)
		}
		if _, ok := nodeIDs[tgt]; !ok {
			return fmt.Errorf("边引用未知 target: %s", tgt)
		}
		if src == tgt {
			return fmt.Errorf("禁止自环: %s", src)
		}
		switch e.Data.Condition {
		case "", EdgeOnSuccess, EdgeOnFailure, EdgeAlways:
		default:
			return fmt.Errorf("边 %s→%s 条件无效: %s", src, tgt, e.Data.Condition)
		}
		succ[src] = append(succ[src], tgt)
		indeg[tgt]++
	}

	for id := range nodeIDs {
		switch types[id] {
		case PipelineNodeStart:
			if indeg[id] > 0 {
				return fmt.Errorf("开始节点 %s 不能有入边", id)
			}
		case PipelineNodeEnd:
			if len(succ[id]) > 0 {
				return fmt.Errorf("结束节点 %s 不能有出边", id)
			}
			if indeg[id] == 0 {
				return fmt.Errorf("结束节点 %s 缺少入边", id)
			}
		default:
			if indeg[id] == 0 {
				return fmt.Errorf("节点 %s 缺少入边（只能从开始节点出发）", id)
			}
		}
	}

	queue := make([]string, 0, len(g.Nodes))
	for id, d := range indeg {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range succ[cur] {
			indeg[next]--
			if indeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited != len(g.Nodes) {
		return errorsNew("流水线图存在环，必须为 DAG")
	}
	return nil
}

// GraphAdjacency returns successor/predecessor edge maps keyed by node id.
func GraphAdjacency(g *PipelineGraph) (succ, pred map[string][]PipelineGraphEdge) {
	succ = make(map[string][]PipelineGraphEdge)
	pred = make(map[string][]PipelineGraphEdge)
	if g == nil {
		return succ, pred
	}
	for _, n := range g.Nodes {
		id := strings.TrimSpace(n.ID)
		succ[id] = nil
		pred[id] = nil
	}
	for _, e := range g.Edges {
		src := strings.TrimSpace(e.Source)
		tgt := strings.TrimSpace(e.Target)
		succ[src] = append(succ[src], e)
		pred[tgt] = append(pred[tgt], e)
	}
	return succ, pred
}

// RootNodeIDs returns nodes with indegree 0.
func RootNodeIDs(g *PipelineGraph) []string {
	_, pred := GraphAdjacency(g)
	roots := make([]string, 0)
	for id, ps := range pred {
		if len(ps) == 0 {
			roots = append(roots, id)
		}
	}
	return roots
}

// NodeByID indexes graph nodes by id.
func NodeByID(g *PipelineGraph) map[string]PipelineGraphNode {
	m := make(map[string]PipelineGraphNode, len(g.Nodes))
	for _, n := range g.Nodes {
		m[n.ID] = n
	}
	return m
}
