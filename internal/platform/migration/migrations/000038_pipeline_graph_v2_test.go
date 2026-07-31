package migrations

import (
	"encoding/json"
	"strings"
	"testing"
)

type upgradeTestGraph struct {
	Nodes []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"nodes"`
	Edges []struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Data   struct {
			Condition string `json:"condition"`
		} `json:"data"`
	} `json:"edges"`
}

func parseUpgraded(t *testing.T, raw string) upgradeTestGraph {
	t.Helper()
	out, err := UpgradePipelineGraphJSON(raw)
	if err != nil {
		t.Fatalf("UpgradePipelineGraphJSON: %v", err)
	}
	var g upgradeTestGraph
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatalf("upgraded graph invalid JSON: %v", err)
	}
	return g
}

func TestUpgradePipelineGraphJSON_WrapsLegacyGraph(t *testing.T) {
	legacy := `{"nodes":[` +
		`{"id":"a","type":"buildJob","position":{"x":100,"y":100},"data":{"build_job_id":1}},` +
		`{"id":"b","type":"buildJob","position":{"x":400,"y":100},"data":{"build_job_id":2}},` +
		`{"id":"c","type":"buildJob","position":{"x":700,"y":100},"data":{"build_job_id":3}}` +
		`],"edges":[` +
		`{"id":"e1","source":"a","target":"b"},` +
		`{"id":"e2","source":"b","target":"c"}` +
		`]}`
	g := parseUpgraded(t, legacy)

	var starts, ends int
	types := map[string]string{}
	for _, n := range g.Nodes {
		types[n.ID] = n.Type
		if n.Type == "start" {
			starts++
		}
		if n.Type == "end" {
			ends++
		}
	}
	if starts != 1 || ends != 1 {
		t.Fatalf("want 1 start + 1 end, got start=%d end=%d", starts, ends)
	}
	// start → a (the only root); c → end (the only leaf).
	wantEdges := map[string]bool{"__start__>a": false, "c>__end__": false}
	for _, e := range g.Edges {
		key := e.Source + ">" + e.Target
		if _, ok := wantEdges[key]; ok {
			wantEdges[key] = true
			if e.Data.Condition != "on_success" {
				t.Fatalf("wrapper edge %s condition=%q", key, e.Data.Condition)
			}
		}
	}
	for k, found := range wantEdges {
		if !found {
			t.Fatalf("missing wrapper edge %s (edges=%v)", k, g.Edges)
		}
	}
	// Old edges untouched: a→b, b→c still present without injected conditions.
	if types["a"] != "buildJob" || types["c"] != "buildJob" {
		t.Fatalf("legacy node types changed: %v", types)
	}
}

func TestUpgradePipelineGraphJSON_MultiRootMultiLeaf(t *testing.T) {
	legacy := `{"nodes":[` +
		`{"id":"a","type":"buildJob","position":{"x":0,"y":0},"data":{"build_job_id":1}},` +
		`{"id":"b","type":"buildJob","position":{"x":0,"y":200},"data":{"build_job_id":2}}` +
		`],"edges":[]}`
	g := parseUpgraded(t, legacy)
	got := map[string]bool{}
	for _, e := range g.Edges {
		got[e.Source+">"+e.Target] = true
	}
	for _, want := range []string{"__start__>a", "__start__>b", "a>__end__", "b>__end__"} {
		if !got[want] {
			t.Fatalf("missing edge %s (edges=%v)", want, g.Edges)
		}
	}
}

func TestUpgradePipelineGraphJSON_EmptyGraph(t *testing.T) {
	for _, raw := range []string{"", `{"nodes":[],"edges":[]}`} {
		g := parseUpgraded(t, raw)
		if len(g.Nodes) != 2 || len(g.Edges) != 0 {
			t.Fatalf("empty graph: want start+end only, got %+v", g)
		}
		if g.Nodes[0].Type != "start" || g.Nodes[1].Type != "end" {
			t.Fatalf("empty graph seed wrong: %+v", g.Nodes)
		}
	}
}

func TestUpgradePipelineGraphJSON_AlreadyV2Unchanged(t *testing.T) {
	v2 := `{"nodes":[{"id":"start","type":"start","position":{"x":0,"y":0},"data":{}}],"edges":[]}`
	out, err := UpgradePipelineGraphJSON(v2)
	if err != nil {
		t.Fatal(err)
	}
	if out != v2 {
		t.Fatalf("v2 graph should be unchanged, got %s", out)
	}
}

func TestUpgradePipelineGraphJSON_IDCollision(t *testing.T) {
	legacy := `{"nodes":[` +
		`{"id":"__start__","type":"buildJob","position":{"x":0,"y":0},"data":{"build_job_id":1}}` +
		`],"edges":[]}`
	g := parseUpgraded(t, legacy)
	seen := map[string]int{}
	for _, n := range g.Nodes {
		seen[n.ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Fatalf("duplicate node id %s", id)
		}
	}
	if !strings.Contains(strings.Join(nodeIDs(g), ","), "__start__2") {
		t.Fatalf("collision not resolved: %v", g.Nodes)
	}
}

func nodeIDs(g upgradeTestGraph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		ids = append(ids, n.ID)
	}
	return ids
}
