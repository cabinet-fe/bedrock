package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PipelineGraph is the persisted VueFlow nodes/edges shape (subset used for validation).
type PipelineGraph struct {
	Nodes []PipelineGraphNode `json:"nodes"`
	Edges []PipelineGraphEdge `json:"edges"`
}

type PipelineGraphNode struct {
	ID   string            `json:"id"`
	Data PipelineNodeData  `json:"data"`
	Type string            `json:"type,omitempty"`
}

type PipelineNodeData struct {
	BuildJobID uint   `json:"build_job_id"`
	Label      string `json:"label,omitempty"`
}

type PipelineGraphEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// JobExistsFunc reports whether a build job id exists.
type JobExistsFunc func(id uint) bool

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

// ValidatePipelineDAG checks node/edge integrity, build_job_id presence, and acyclicity.
func ValidatePipelineDAG(g *PipelineGraph, jobExists JobExistsFunc) error {
	if g == nil {
		return errorsNew("graph_json 不能为空")
	}
	if len(g.Nodes) == 0 {
		return errorsNew("流水线至少需要一个节点")
	}

	nodeIDs := make(map[string]struct{}, len(g.Nodes))
	for i, n := range g.Nodes {
		id := strings.TrimSpace(n.ID)
		if id == "" {
			return fmt.Errorf("节点 #%d 缺少 id", i+1)
		}
		if _, dup := nodeIDs[id]; dup {
			return fmt.Errorf("重复节点 id: %s", id)
		}
		nodeIDs[id] = struct{}{}
		if n.Data.BuildJobID == 0 {
			return fmt.Errorf("节点 %s 缺少 build_job_id", id)
		}
		if jobExists != nil && !jobExists(n.Data.BuildJobID) {
			return fmt.Errorf("节点 %s 引用的构建任务不存在: %d", id, n.Data.BuildJobID)
		}
	}

	// adjacency + indegree for Kahn
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
		succ[src] = append(succ[src], tgt)
		indeg[tgt]++
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

// GraphAdjacency returns successors and predecessors maps.
func GraphAdjacency(g *PipelineGraph) (succ, pred map[string][]string) {
	succ = make(map[string][]string)
	pred = make(map[string][]string)
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
		succ[src] = append(succ[src], tgt)
		pred[tgt] = append(pred[tgt], src)
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
