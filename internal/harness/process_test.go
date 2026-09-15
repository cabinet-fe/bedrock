package harness

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// fakeServeBin is built once per run and stands in for `opencode serve`.
var fakeServeBin string

var testClient = &http.Client{Timeout: 2 * time.Second}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bedrock-harness-fakeserve-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	fakeServeBin = filepath.Join(dir, "fakeserve")
	if runtime.GOOS == "windows" {
		fakeServeBin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", fakeServeBin, "bedrock/internal/harness/testdata/fakeserve").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "building fakeserve: %v\n%s", err, out)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// newManager builds a fast-probing manager and registers Stop as cleanup.
func newManager(t *testing.T, bin string) *ProcessManager {
	t.Helper()
	m := NewProcessManager(ProcessConfig{
		Bin:            bin,
		Port:           freePort(t),
		PasswordFile:   filepath.Join(t.TempDir(), "server-password"),
		HealthInterval: 100 * time.Millisecond,
		RestartDelay:   300 * time.Millisecond,
	}, zap.NewNop())
	t.Cleanup(m.Stop)
	return m
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func startCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 15*time.Second)
}

// waitFor polls the manager status until pred holds and returns the elapsed
// time; it fails the test when the timeout is hit first.
func waitFor(t *testing.T, m *ProcessManager, pred func(ProcessStatus) bool, timeout time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	for deadline := time.Now().Add(timeout); ; {
		if pred(m.Status()) {
			return time.Since(start)
		}
		if time.Now().After(deadline) {
			t.Fatalf("status %+v not matching within %s", m.Status(), timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestProcessStartsHealthyOnLoopback covers the P2 acceptance: the managed
// serve only binds 127.0.0.1 and the random password is persisted with
// tight file permissions.
func TestProcessStartsHealthyOnLoopback(t *testing.T) {
	m := newManager(t, fakeServeBin)
	addrFile := filepath.Join(t.TempDir(), "addr")
	t.Setenv("FAKE_SERVE_ADDR_FILE", addrFile)

	ctx, cancel := startCtx(t)
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if state := m.Status().State; state != StatusOK {
		t.Fatalf("state = %q, want ok", state)
	}

	// the process reports the address it actually bound: loopback only
	addr := waitForFile(t, addrFile)
	if host, _, err := net.SplitHostPort(addr); err != nil || host != "127.0.0.1" {
		t.Fatalf("serve bound %q, want host 127.0.0.1", addr)
	}

	// the persisted password is what the process actually enforces
	raw, err := os.ReadFile(m.cfg.PasswordFile)
	if err != nil {
		t.Fatalf("password file: %v", err)
	}
	password := strings.TrimSpace(string(raw))
	if len(password) != 2*passwordBytes {
		t.Fatalf("password length = %d, want %d hex chars", len(password), 2*passwordBytes)
	}
	if info, err := os.Stat(m.cfg.PasswordFile); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("password file mode = %v, want 0600 (err %v)", info.Mode().Perm(), err)
	}
	probeURL := fmt.Sprintf("http://127.0.0.1:%d/global/health", m.cfg.Port)
	if code := healthStatus(t, probeURL, password); code != http.StatusOK {
		t.Fatalf("health with persisted password = %d, want 200", code)
	}
	if code := healthStatus(t, probeURL, "wrong-password"); code != http.StatusUnauthorized {
		t.Fatalf("health with wrong password = %d, want 401", code)
	}
}

// TestProcessPasswordPersistedAcrossRestart verifies the supervisor reuses
// the stored password instead of rotating it on every start.
func TestProcessPasswordPersistedAcrossRestart(t *testing.T) {
	m := newManager(t, fakeServeBin)
	ctx, cancel := startCtx(t)
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	first := m.Password()
	m.Stop()

	second := newManager(t, fakeServeBin)
	second.cfg.PasswordFile = m.cfg.PasswordFile
	ctx2, cancel2 := startCtx(t)
	defer cancel2()
	if err := second.Start(ctx2); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if got := second.Password(); got != first {
		t.Fatalf("password rotated across restart: %q != %q", got, first)
	}
}

// TestProcessCrashAutoRestart kills the serve process and requires recovery
// within the 60s budget from the spec.
func TestProcessCrashAutoRestart(t *testing.T) {
	m := newManager(t, fakeServeBin)
	ctx, cancel := startCtx(t)
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	resp, err := testClient.Get(fmt.Sprintf("http://127.0.0.1:%d/global/crash", m.cfg.Port))
	if err == nil {
		resp.Body.Close()
	}
	// the crash must be observed before recovery is measured
	waitFor(t, m, func(s ProcessStatus) bool {
		return s.State == StatusDegraded || s.Restarts > 0
	}, 10*time.Second)
	recovered := waitFor(t, m, func(s ProcessStatus) bool { return s.State == StatusOK }, 60*time.Second)
	if status := m.Status(); status.Restarts == 0 {
		t.Fatalf("crash not counted as restart (status %+v)", status)
	}
	if recovered >= 60*time.Second {
		t.Fatalf("recovery took %s, want < 60s", recovered)
	}
}

// TestProcessDegradedWhenUnavailable covers the degraded reporting when the
// backend cannot run at all.
func TestProcessDegradedWhenUnavailable(t *testing.T) {
	m := newManager(t, filepath.Join(t.TempDir(), "missing-serve"))
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if err := m.Start(ctx); err == nil {
		t.Fatal("expected start error for missing bin")
	}
	status := m.Status()
	if status.State != StatusDegraded {
		t.Fatalf("state = %q, want degraded", status.State)
	}
	if status.LastError == "" {
		t.Fatal("degraded status should carry the spawn error")
	}
}

// TestProcessStopTerminatesServe verifies graceful shutdown leaves nothing
// behind and Stop is idempotent.
func TestProcessStopTerminatesServe(t *testing.T) {
	m := newManager(t, fakeServeBin)
	ctx, cancel := startCtx(t)
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	port := m.cfg.Port
	m.Stop()
	m.Stop() // second Stop must be a no-op

	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), 200*time.Millisecond)
		if err != nil {
			break // port no longer accepting: process is gone
		}
		conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("serve still accepting connections after Stop")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func healthStatus(t *testing.T, url, password string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("opencode", password)
	resp, err := testClient.Do(req)
	if err != nil {
		t.Fatalf("health probe: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func waitForFile(t *testing.T, path string) string {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; {
		raw, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(raw))
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %s never appeared", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
