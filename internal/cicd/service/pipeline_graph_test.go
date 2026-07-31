package service

import (
	"strings"
	"testing"

	"bedrock/internal/pkg"
)

func v2Refs() PipelineRefChecker {
	return PipelineRefChecker{
		BuildJobExists:  func(id uint) bool { return id >= 1 && id <= 3 },
		ScriptJobExists: func(id uint) bool { return id >= 1 && id <= 3 },
		AgentExists:     func(id uint) bool { return id >= 1 && id <= 3 },
	}
}

func TestValidatePipelineDAG_OK(t *testing.T) {
	g := &PipelineGraph{
		Nodes: []PipelineGraphNode{
			{ID: "start", Type: "start"},
			{ID: "a", Type: "buildJob", Data: PipelineNodeData{BuildJobID: 1}},
			{ID: "b", Type: "scriptJob", Data: PipelineNodeData{ScriptJobID: 2}},
			{ID: "c", Type: "agent", Data: PipelineNodeData{AgentID: 3}},
			{ID: "end", Type: "end"},
		},
		Edges: []PipelineGraphEdge{
			{ID: "e0", Source: "start", Target: "a"},
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "a", Target: "c", Data: PipelineEdgeData{Condition: "on_failure"}},
			{ID: "e3", Source: "b", Target: "end"},
			{ID: "e4", Source: "c", Target: "end", Data: PipelineEdgeData{Condition: "always"}},
		},
	}
	if err := ValidatePipelineDAG(g, v2Refs()); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestValidatePipelineDAG_LegacyTypelessNodeIsBuildJob(t *testing.T) {
	g := &PipelineGraph{
		Nodes: []PipelineGraphNode{
			{ID: "start", Type: "start"},
			{ID: "a", Data: PipelineNodeData{BuildJobID: 1}},
			{ID: "end", Type: "end"},
		},
		Edges: []PipelineGraphEdge{
			{ID: "e0", Source: "start", Target: "a"},
			{ID: "e1", Source: "a", Target: "end"},
		},
	}
	if err := ValidatePipelineDAG(g, v2Refs()); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestValidatePipelineDAG_Cycle(t *testing.T) {
	g := &PipelineGraph{
		Nodes: []PipelineGraphNode{
			{ID: "start", Type: "start"},
			{ID: "a", Type: "buildJob", Data: PipelineNodeData{BuildJobID: 1}},
			{ID: "b", Type: "buildJob", Data: PipelineNodeData{BuildJobID: 2}},
			{ID: "end", Type: "end"},
		},
		Edges: []PipelineGraphEdge{
			{ID: "e0", Source: "start", Target: "a"},
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "a"},
			{ID: "e3", Source: "b", Target: "end"},
		},
	}
	if err := ValidatePipelineDAG(g, v2Refs()); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestValidatePipelineDAG_MissingRefs(t *testing.T) {
	cases := []struct {
		name string
		node PipelineGraphNode
	}{
		{"buildJob", PipelineGraphNode{ID: "a", Type: "buildJob", Data: PipelineNodeData{BuildJobID: 99}}},
		{"scriptJob", PipelineGraphNode{ID: "a", Type: "scriptJob", Data: PipelineNodeData{ScriptJobID: 99}}},
		{"agent", PipelineGraphNode{ID: "a", Type: "agent", Data: PipelineNodeData{AgentID: 99}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &PipelineGraph{
				Nodes: []PipelineGraphNode{
					{ID: "start", Type: "start"}, tc.node, {ID: "end", Type: "end"},
				},
				Edges: []PipelineGraphEdge{
					{ID: "e0", Source: "start", Target: "a"},
					{ID: "e1", Source: "a", Target: "end"},
				},
			}
			if err := ValidatePipelineDAG(g, v2Refs()); err == nil {
				t.Fatal("expected missing-ref error")
			}
		})
	}
}

func TestValidatePipelineDAG_Structure(t *testing.T) {
	mk := func(nodes []PipelineGraphNode, edges []PipelineGraphEdge) *PipelineGraph {
		return &PipelineGraph{Nodes: nodes, Edges: edges}
	}
	start := PipelineGraphNode{ID: "start", Type: "start"}
	start2 := PipelineGraphNode{ID: "start2", Type: "start"}
	end := PipelineGraphNode{ID: "end", Type: "end"}
	a := PipelineGraphNode{ID: "a", Type: "buildJob", Data: PipelineNodeData{BuildJobID: 1}}

	cases := []struct {
		name    string
		g       *PipelineGraph
		wantErr string
	}{
		{"empty", &PipelineGraph{}, "至少需要一个节点"},
		{"no start", mk([]PipelineGraphNode{a, end}, []PipelineGraphEdge{{ID: "e1", Source: "a", Target: "end"}}), "恰好 1 个开始节点"},
		{"two starts", mk(
			[]PipelineGraphNode{start, start2, a, end},
			[]PipelineGraphEdge{{ID: "e0", Source: "start", Target: "a"}, {ID: "e1", Source: "a", Target: "end"}},
		), "恰好 1 个开始节点"},
		{"no end", mk(
			[]PipelineGraphNode{start, a},
			[]PipelineGraphEdge{{ID: "e0", Source: "start", Target: "a"}},
		), "至少需要 1 个结束节点"},
		{"start with inedge", mk(
			[]PipelineGraphNode{start, a, end},
			[]PipelineGraphEdge{{ID: "e0", Source: "a", Target: "start"}, {ID: "e1", Source: "start", Target: "a"}, {ID: "e2", Source: "a", Target: "end"}},
		), "不能有入边"},
		{"end with outedge", mk(
			[]PipelineGraphNode{start, a, end},
			[]PipelineGraphEdge{{ID: "e0", Source: "start", Target: "a"}, {ID: "e1", Source: "a", Target: "end"}, {ID: "e2", Source: "end", Target: "a"}},
		), "不能有出边"},
		{"orphan node", mk(
			[]PipelineGraphNode{start, a, PipelineGraphNode{ID: "b", Type: "buildJob", Data: PipelineNodeData{BuildJobID: 2}}, end},
			[]PipelineGraphEdge{{ID: "e0", Source: "start", Target: "a"}, {ID: "e1", Source: "a", Target: "end"}},
		), "缺少入边"},
		{"self loop", mk(
			[]PipelineGraphNode{start, a, end},
			[]PipelineGraphEdge{{ID: "e0", Source: "start", Target: "a"}, {ID: "e1", Source: "a", Target: "a"}, {ID: "e2", Source: "a", Target: "end"}},
		), "自环"},
		{"bad condition", mk(
			[]PipelineGraphNode{start, a, end},
			[]PipelineGraphEdge{
				{ID: "e0", Source: "start", Target: "a"},
				{ID: "e1", Source: "a", Target: "end", Data: PipelineEdgeData{Condition: "sometimes"}},
			},
		), "条件无效"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePipelineDAG(tc.g, v2Refs())
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q should contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidatePipelineDAG_InvalidEnvKey(t *testing.T) {
	g := &PipelineGraph{
		Nodes: []PipelineGraphNode{
			{ID: "start", Type: "start"},
			{ID: "a", Type: "buildJob", Data: PipelineNodeData{BuildJobID: 1, EnvVars: []PipelineNodeEnvVar{{Key: "BAD=KEY", Value: "x"}}}},
			{ID: "end", Type: "end"},
		},
		Edges: []PipelineGraphEdge{
			{ID: "e0", Source: "start", Target: "a"},
			{ID: "e1", Source: "a", Target: "end"},
		},
	}
	if err := ValidatePipelineDAG(g, v2Refs()); err == nil {
		t.Fatal("expected env key error")
	}
}

func TestEdgeConditionMatches(t *testing.T) {
	cases := []struct {
		cond, status string
		want         bool
	}{
		{"", "success", true},
		{"on_success", "success", true},
		{"on_success", "failed", false},
		{"on_failure", "failed", true},
		{"on_failure", "cancelled", true},
		{"on_failure", "interrupted", true},
		{"on_failure", "success", false},
		{"always", "success", true},
		{"always", "failed", true},
		{"always", "skipped", false},
		{"bogus", "success", false},
	}
	for _, tc := range cases {
		if got := EdgeConditionMatches(tc.cond, tc.status); got != tc.want {
			t.Fatalf("EdgeConditionMatches(%q,%q)=%v, want %v", tc.cond, tc.status, got, tc.want)
		}
	}
}

func TestRootNodeIDs(t *testing.T) {
	g := &PipelineGraph{
		Nodes: []PipelineGraphNode{{ID: "a"}, {ID: "b"}},
		Edges: []PipelineGraphEdge{{ID: "e1", Source: "a", Target: "b"}},
	}
	roots := RootNodeIDs(g)
	if len(roots) != 1 || roots[0] != "a" {
		t.Fatalf("roots=%v", roots)
	}
}

func TestGraphEnvVarsEncryptSanitizeDecrypt(t *testing.T) {
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := pkg.InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	raw := `{"nodes":[` +
		`{"id":"start","type":"start","position":{"x":0,"y":0},"data":{}},` +
		`{"id":"a","type":"buildJob","position":{"x":100,"y":0},"data":{"build_job_id":1,"env_vars":[{"key":"FOO","value":"bar"},{"key":"KEEP"}]}},` +
		`{"id":"end","type":"end","position":{"x":200,"y":0},"data":{}}` +
		`],"edges":[{"id":"e0","source":"start","target":"a"},{"id":"e1","source":"a","target":"end"}]}`
	// Old graph already stores an encrypted KEEP value under node a.
	old := `{"nodes":[{"id":"a","type":"buildJob","data":{"build_job_id":1,"env_vars":[{"key":"KEEP","value":"enc:v1:deadbeef"}]}}],"edges":[]}`

	stored, err := EncryptGraphEnvVars(raw, old)
	if err != nil {
		t.Fatal(err)
	}
	g, err := ParsePipelineGraph(stored)
	if err != nil {
		t.Fatal(err)
	}
	node := NodeByID(g)["a"]
	if len(node.Data.EnvVars) != 2 {
		t.Fatalf("env vars lost: %+v", node.Data.EnvVars)
	}
	if !strings.HasPrefix(node.Data.EnvVars[0].Value, "enc:v1:") || node.Data.EnvVars[0].Value == "bar" {
		t.Fatalf("FOO not encrypted: %q", node.Data.EnvVars[0].Value)
	}
	if node.Data.EnvVars[1].Value != "enc:v1:deadbeef" {
		t.Fatalf("KEEP should inherit stored cipher, got %q", node.Data.EnvVars[1].Value)
	}
	if len(node.Position) == 0 {
		t.Fatal("position lost after encrypt round-trip")
	}

	// Sanitize hides values, keeps keys.
	sanitized := SanitizeGraphEnvVars(stored)
	sg, _ := ParsePipelineGraph(sanitized)
	senv := NodeByID(sg)["a"].Data.EnvVars
	for _, kv := range senv {
		if kv.Value != "" {
			t.Fatalf("sanitize leaked value for %s", kv.Key)
		}
		if !kv.HasValue {
			t.Fatalf("sanitize should set has_value for %s", kv.Key)
		}
	}

	// Decrypt resolves plaintext for enqueue.
	vars, err := DecryptNodeEnvVars(node.Data.EnvVars[:1])
	if err != nil {
		t.Fatal(err)
	}
	if vars["FOO"] != "bar" {
		t.Fatalf("decrypt FOO=%q", vars["FOO"])
	}
}

func TestEncryptGraphEnvVars_NewKeyWithoutValueFails(t *testing.T) {
	raw := `{"nodes":[{"id":"a","type":"buildJob","data":{"build_job_id":1,"env_vars":[{"key":"NEW"}]}}],"edges":[]}`
	if _, err := EncryptGraphEnvVars(raw, ""); err == nil {
		t.Fatal("expected must-provide-value error")
	}
}
