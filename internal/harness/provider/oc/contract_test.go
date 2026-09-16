package oc

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bedrock/internal/harness/provider"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// serveEnv returns client config for a real `opencode serve`, skipping the
// test when the opt-in environment is absent.
func serveEnv(t *testing.T) Config {
	t.Helper()
	addr := os.Getenv("HARNESS_OC_TEST_ADDR")
	if addr == "" {
		t.Skip("set HARNESS_OC_TEST_ADDR (and optionally HARNESS_OC_TEST_PASSWORD, HARNESS_OC_TEST_USERNAME) to run contract tests against a real opencode serve")
	}
	return Config{
		BaseURL:  addr,
		Password: os.Getenv("HARNESS_OC_TEST_PASSWORD"),
		Username: os.Getenv("HARNESS_OC_TEST_USERNAME"),
	}
}

// canonicalJSON re-encodes v with sorted object keys so two documents that
// differ only in key order compare equal.
func canonicalJSON(t *testing.T, v json.RawMessage) string {
	t.Helper()
	var doc any
	if err := json.Unmarshal(v, &doc); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	return string(out)
}

// TestContractSpecSnapshot diffs GET /doc against the golden OpenAPI file.
// The golden was captured from opencode v1.18.29; a mismatch means the serve
// contract drifted and the adapter must be reviewed (harness-integration-
// plan.md §2.3).
func TestContractSpecSnapshot(t *testing.T) {
	cfg := serveEnv(t)
	client := NewClient(cfg)
	raw, err := client.Spec(context.Background())
	if err != nil {
		t.Fatalf("fetch /doc: %v", err)
	}
	got := canonicalJSON(t, raw)

	goldenPath := filepath.Join("testdata", "openapi-spec.golden.json")
	if *updateGolden {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		specFile := filepath.Join(t.TempDir(), "openapi-actual.json")
		_ = os.WriteFile(specFile, []byte(got), 0o644)
		t.Fatalf("GET /doc spec drifted from golden (opencode upgraded?); actual spec saved to %s — review the diff and refresh the golden with -update", specFile)
	}

	// Guard the endpoints the adapter relies on, so drift fails with a
	// readable message even if the golden was blindly refreshed.
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	required := []string{
		"/api/session",
		"/api/session/{sessionID}/prompt",
		"/api/session/{sessionID}/wait",
		"/api/session/{sessionID}/interrupt",
		"/api/session/{sessionID}/event",
		"/api/session/{sessionID}/history",
		"/api/session/{sessionID}/message",
		"/api/session/{sessionID}/permission/{requestID}/reply",
		"/api/session/{sessionID}/question/{requestID}/reply",
		"/api/permission/request",
		"/api/question/request",
		"/api/provider",
		"/api/model",
		"/api/agent",
		"/api/event",
		"/global/health",
	}
	for _, p := range required {
		if _, ok := doc.Paths[p]; !ok {
			t.Errorf("spec no longer documents %s", p)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
}

// TestContractHealth verifies Basic Auth against a real serve.
func TestContractHealth(t *testing.T) {
	cfg := serveEnv(t)
	client := NewClient(cfg)
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if !health.Healthy {
		t.Fatalf("serve reports unhealthy: %+v", health)
	}
	if health.Version == "" {
		t.Fatal("health response missing version")
	}
	t.Logf("serve version: %s", health.Version)
}

// testSessionInput builds a session input bound to a throwaway directory.
func testSessionInput(t *testing.T) provider.CreateSessionInput {
	t.Helper()
	return provider.CreateSessionInput{Directory: t.TempDir()}
}

// TestContractWaitUnavailable pins the v1.18.29 finding that POST wait
// answers 503 for every call (harness-integration-plan.md §2.2): idle
// detection must be event-driven.
func TestContractWaitUnavailable(t *testing.T) {
	cfg := serveEnv(t)
	client := NewClient(cfg)
	ctx := context.Background()
	session, err := client.CreateSession(ctx, testSessionInput(t))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	err = client.Wait(ctx, session.ID)
	if err == nil {
		t.Skip("serve now implements wait; update harness-integration-plan.md §2.2 and drop this pin")
	}
	t.Logf("wait error (expected unavailable on 1.18.29): %v", err)
}

// TestContractActiveSessions pins the v1.18.29 wire shape of GET
// /api/session/active ("sessions absent from the result are inactive"): a
// freshly created session must not appear in the busy set. This query is
// what the stream bridge settles turns against, because the event stream
// never broadcasts session.idle on 1.18.x.
func TestContractActiveSessions(t *testing.T) {
	cfg := serveEnv(t)
	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := client.CreateSession(ctx, testSessionInput(t))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	active, err := client.ActiveSessions(ctx)
	if err != nil {
		t.Fatalf("active sessions: %v", err)
	}
	if active[session.ID] {
		t.Fatalf("fresh session %s reported active: %v", session.ID, active)
	}
	t.Logf("active set: %v", active)
}
