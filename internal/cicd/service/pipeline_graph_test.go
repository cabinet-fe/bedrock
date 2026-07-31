package service

import "testing"

func TestValidatePipelineDAG_OK(t *testing.T) {
	g := &PipelineGraph{
		Nodes: []PipelineGraphNode{
			{ID: "a", Data: PipelineNodeData{BuildJobID: 1}},
			{ID: "b", Data: PipelineNodeData{BuildJobID: 2}},
			{ID: "c", Data: PipelineNodeData{BuildJobID: 3}},
		},
		Edges: []PipelineGraphEdge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "a", Target: "c"},
		},
	}
	exists := func(id uint) bool { return id >= 1 && id <= 3 }
	if err := ValidatePipelineDAG(g, exists); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestValidatePipelineDAG_Cycle(t *testing.T) {
	g := &PipelineGraph{
		Nodes: []PipelineGraphNode{
			{ID: "a", Data: PipelineNodeData{BuildJobID: 1}},
			{ID: "b", Data: PipelineNodeData{BuildJobID: 2}},
		},
		Edges: []PipelineGraphEdge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "a"},
		},
	}
	exists := func(uint) bool { return true }
	if err := ValidatePipelineDAG(g, exists); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestValidatePipelineDAG_MissingJob(t *testing.T) {
	g := &PipelineGraph{
		Nodes: []PipelineGraphNode{
			{ID: "a", Data: PipelineNodeData{BuildJobID: 99}},
		},
	}
	exists := func(uint) bool { return false }
	if err := ValidatePipelineDAG(g, exists); err == nil {
		t.Fatal("expected missing job error")
	}
}

func TestValidatePipelineDAG_SelfLoop(t *testing.T) {
	g := &PipelineGraph{
		Nodes: []PipelineGraphNode{
			{ID: "a", Data: PipelineNodeData{BuildJobID: 1}},
		},
		Edges: []PipelineGraphEdge{
			{ID: "e1", Source: "a", Target: "a"},
		},
	}
	exists := func(uint) bool { return true }
	if err := ValidatePipelineDAG(g, exists); err == nil {
		t.Fatal("expected self-loop error")
	}
}

func TestValidatePipelineDAG_Empty(t *testing.T) {
	if err := ValidatePipelineDAG(&PipelineGraph{}, func(uint) bool { return true }); err == nil {
		t.Fatal("expected empty graph error")
	}
}

func TestRootNodeIDs(t *testing.T) {
	g := &PipelineGraph{
		Nodes: []PipelineGraphNode{
			{ID: "a", Data: PipelineNodeData{BuildJobID: 1}},
			{ID: "b", Data: PipelineNodeData{BuildJobID: 2}},
		},
		Edges: []PipelineGraphEdge{
			{ID: "e1", Source: "a", Target: "b"},
		},
	}
	roots := RootNodeIDs(g)
	if len(roots) != 1 || roots[0] != "a" {
		t.Fatalf("roots=%v", roots)
	}
}
