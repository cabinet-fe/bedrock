package service_test

import (
	"errors"
	"testing"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/service"
	rbacmodel "bedrock/internal/rbac/model"
)

// Agent data-scope semantics: self-scope actors only list/read/mutate their
// own agents (and runs thereof); all-scope actors are unrestricted.
func TestAgentDataScope(t *testing.T) {
	_, agents, _, _, _ := setupAI(t)

	own, err := agents.CreateAgent(1, service.AgentInput{Name: "own"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := agents.CreateAgent(2, service.AgentInput{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}

	self := service.AgentActor{UserID: 1, DataScope: rbacmodel.DataScopeSelf}
	all := service.AgentActor{UserID: 1, DataScope: rbacmodel.DataScopeAll}

	items, total, err := agents.ListAgents(1, 20, self)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != own.ID {
		t.Fatalf("self list want [own] got total=%d ids=%v", total, agentIDs(items))
	}
	items, total, err = agents.ListAgents(1, 20, all)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("all list want 2 got total=%d", total)
	}

	if _, err := agents.GetAgent(other.ID, self); !errors.Is(err, service.ErrAgentForbidden) {
		t.Fatalf("self get other want ErrAgentForbidden got %v", err)
	}
	if _, err := agents.GetAgent(other.ID, all); err != nil {
		t.Fatalf("all get other: %v", err)
	}

	if _, err := agents.UpdateAgent(other.ID, self, service.AgentInput{Name: "hijack"}); !errors.Is(err, service.ErrAgentForbidden) {
		t.Fatalf("self update other want ErrAgentForbidden got %v", err)
	}
	if _, err := agents.CreateTrigger(other.ID, self, service.TriggerInput{Type: "cron", CronExpression: "* * * * *"}); !errors.Is(err, service.ErrAgentForbidden) {
		t.Fatalf("self trigger other want ErrAgentForbidden got %v", err)
	}
	if _, err := agents.ListTriggers(other.ID, self); !errors.Is(err, service.ErrAgentForbidden) {
		t.Fatalf("self list triggers other want ErrAgentForbidden got %v", err)
	}

	ownRun, err := agents.ManualRun(own.ID, self, "hi")
	if err != nil {
		t.Fatal(err)
	}
	otherRun, err := agents.CreateRun(other.ID, service.CreateRunInput{TriggerType: "manual", TriggeredBy: 2})
	if err != nil {
		t.Fatal(err)
	}

	runs, total, err := agents.ListRuns(1, 20, 0, "", nil, self)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(runs) != 1 || runs[0].ID != ownRun.ID {
		t.Fatalf("self run list want [own run] got total=%d ids=%v", total, runIDs(runs))
	}
	if _, err := agents.RequireRunAccess(otherRun.ID, self); !errors.Is(err, service.ErrAgentForbidden) {
		t.Fatalf("self access other run want ErrAgentForbidden got %v", err)
	}
	if _, err := agents.RequireRunAccess(otherRun.ID, all); err != nil {
		t.Fatalf("all access other run: %v", err)
	}

	if err := agents.DeleteAgent(other.ID, self); !errors.Is(err, service.ErrAgentForbidden) {
		t.Fatalf("self delete other want ErrAgentForbidden got %v", err)
	}
	if _, err := agents.GetAgent(other.ID, all); err != nil {
		t.Fatalf("other agent must survive forbidden delete: %v", err)
	}
	if err := agents.DeleteAgent(own.ID, self); err != nil {
		t.Fatalf("self delete own: %v", err)
	}
}

func agentIDs(items []model.AiAgent) []uint {
	ids := make([]uint, 0, len(items))
	for _, a := range items {
		ids = append(ids, a.ID)
	}
	return ids
}

func runIDs(runs []model.AgentRun) []uint {
	ids := make([]uint, 0, len(runs))
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	return ids
}
