package bedctl

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUnitContent(t *testing.T) {
	dir := "/opt/bedrock"
	unit := UnitContent(Server, dir)
	for _, want := range []string{
		"Description=Bedrock server (installed by bedctl)",
		"WorkingDirectory=/opt/bedrock",
		"ExecStart=/opt/bedrock/bedrock --config /opt/bedrock/config.yaml",
		"Restart=on-failure",
		"TimeoutStopSec=45",
		"Environment=\"PATH=",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
	if got := UnitContent(Agent, "/x"); !strings.Contains(got, "TimeoutStopSec=10") {
		t.Fatalf("agent stop timeout wrong:\n%s", got)
	}
	if got := UnitContent(Agent, "/x"); !strings.Contains(got, "ExecStart=/x/bedrock-agent --config /x/bedrock-agent.yaml") {
		t.Fatalf("agent exec wrong:\n%s", got)
	}
}

func TestNohupStopWhenNotRunning(t *testing.T) {
	dir := t.TempDir()
	m := &nohupManager{}
	if m.IsActive(Server, dir) {
		t.Fatal("fresh dir must be inactive")
	}
	if !m.IsDead(Server, dir) {
		t.Fatal("fresh dir must be dead")
	}
	if err := m.Stop(context.Background(), Server, dir, 0); err != nil {
		t.Fatalf("Stop on idle dir: %v", err)
	}
	if _, err := os.Stat(PIDFile(Server, dir)); !os.IsNotExist(err) {
		t.Fatalf("pidfile should be gone: %v", err)
	}
}

func TestNohupStalePidfile(t *testing.T) {
	dir := t.TempDir()
	m := &nohupManager{}
	// PID 2^22-ish that cannot exist in the test environment's namespace;
	// use a definitely-dead pid by spawning and reaping a process.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	dead := cmd.Process.Pid
	if err := os.WriteFile(PIDFile(Server, dir), []byte(strconv.Itoa(dead)), 0o644); err != nil {
		t.Fatal(err)
	}
	if m.IsActive(Server, dir) {
		t.Fatal("dead pid in pidfile must be inactive")
	}
}

func TestTerminatePIDsKillsRealProcess(t *testing.T) {
	// Spawn a long sleeper; SIGTERM should end it well within the grace.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn sleep: %v", err)
	}
	pid := cmd.Process.Pid
	terminatePIDs([]int{pid}, 2*time.Second)
	// Wait must reap the child; give it a moment then check exit.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("process did not terminate after SIGTERM")
	}
	if ProcessAlive(pid) {
		t.Fatal("process reported alive after termination")
	}
}
