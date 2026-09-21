// Package harness hosts the agent session backend. The process manager
// supervises the backend serve subprocess (opencode) so the rest of the
// domain only talks HTTP to a loopback endpoint.
package harness

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"bedrock/internal/harness/provider/oc"
	"bedrock/internal/pkg"
)

// Reported ProcessStatus states.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
)

// bindHost is the only address the serve process may listen on. Not
// configurable: the backend adds no external attack surface.
const bindHost = "127.0.0.1"

const (
	defaultHealthInterval = 5 * time.Second
	defaultRestartDelay   = time.Second
	// maxRestartDelay caps exponential backoff after repeated fast crashes
	// (missing binary, unusable port). 1s * 2^n, reached in 5 crashes.
	maxRestartDelay = 30 * time.Second
	probeTimeout    = 3 * time.Second
	// hungKillThreshold is the consecutive probe-failure count after which a
	// live-but-unresponsive process is killed so the restart loop can recover.
	hungKillThreshold = 3
	shutdownGrace     = 10 * time.Second
	// passwordBytes is the entropy of the generated serve password.
	passwordBytes = 32
)

// ProcessConfig configures the supervised serve process.
type ProcessConfig struct {
	Bin  string
	Port int
	// PasswordFile persists the generated OPENCODE_SERVER_PASSWORD so a
	// supervisor (or bedrock) restart reuses the same secret. Written 0600.
	PasswordFile string
	// HealthInterval overrides the /global/health probe interval (tests).
	HealthInterval time.Duration
	// RestartDelay overrides the crash-restart delay (tests). The recovery
	// budget from the spec is 60s.
	RestartDelay time.Duration
}

func (c *ProcessConfig) healthInterval() time.Duration {
	if c.HealthInterval > 0 {
		return c.HealthInterval
	}
	return defaultHealthInterval
}

func (c *ProcessConfig) restartDelay() time.Duration {
	if c.RestartDelay > 0 {
		return c.RestartDelay
	}
	return defaultRestartDelay
}

// ProcessStatus is the reported state of the managed serve process.
type ProcessStatus struct {
	State     string `json:"state"`
	LastError string `json:"last_error,omitempty"`
	Restarts  int    `json:"restarts"`
}

// ProcessManager starts `{bin} serve --hostname 127.0.0.1 --port {port}`,
// probes /global/health, restarts the process after crashes (within 60s),
// and reports ok/degraded.
type ProcessManager struct {
	cfg ProcessConfig
	log *zap.Logger

	mu       sync.Mutex
	started  bool
	state    string
	lastErr  string
	restarts int
	password string
	kill     func()
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	healthy  chan struct{}
}

// NewProcessManager builds a manager. Call Start to launch the process.
func NewProcessManager(cfg ProcessConfig, log *zap.Logger) *ProcessManager {
	return &ProcessManager{cfg: cfg, log: log, state: StatusDegraded}
}

// BaseURL is the loopback endpoint of the managed process.
func (m *ProcessManager) BaseURL() string {
	return "http://" + net.JoinHostPort(bindHost, strconv.Itoa(m.cfg.Port))
}

// Password returns the persisted Basic Auth password for provider clients.
func (m *ProcessManager) Password() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.password
}

// Status reports the current ok/degraded state.
func (m *ProcessManager) Status() ProcessStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return ProcessStatus{State: m.state, LastError: m.lastErr, Restarts: m.restarts}
}

func (m *ProcessManager) setState(state, lastErr string) {
	m.mu.Lock()
	changed := m.state != state || m.lastErr != lastErr
	m.state, m.lastErr = state, lastErr
	m.mu.Unlock()
	if !changed {
		return
	}
	if state == StatusOK {
		m.log.Info("harness serve healthy", zap.String("base_url", m.BaseURL()))
		return
	}
	m.log.Warn("harness serve degraded", zap.String("base_url", m.BaseURL()), zap.String("error", lastErr))
}

// Start launches the supervisor goroutines and blocks until the first
// successful health probe, ctx is done, or Stop is called. Callers that must
// not delay startup use StartBackground instead.
func (m *ProcessManager) Start(ctx context.Context) error {
	m.startBackground()
	select {
	case <-m.healthyWait():
		return nil
	case <-ctx.Done():
		return fmt.Errorf("harness serve not healthy: %s", m.Status().LastError)
	}
}

// StartBackground launches the serve supervisor and returns immediately; the
// supervisor keeps retrying until healthy or Stop. Callers that must not
// delay the HTTP listener (server main) use this and let harness converge in
// the background; Start stays for tests and explicit waiters.
func (m *ProcessManager) StartBackground() { m.startBackground() }

func (m *ProcessManager) startBackground() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	password, err := loadOrCreatePassword(m.cfg.PasswordFile)
	if err != nil {
		m.mu.Unlock()
		m.log.Error("harness serve password unavailable; running degraded", zap.Error(err))
		return
	}
	m.started = true
	m.password = password
	m.stopCh = make(chan struct{})
	// healthy is captured locally: Start must not re-read the m.healthy
	// field after the goroutines start, because markHealthy nils it under
	// the mutex. A local copy is safe to receive from even if it is
	// already closed by then.
	healthy := make(chan struct{})
	m.healthy = healthy
	m.mu.Unlock()

	m.log.Info("harness serve starting",
		zap.String("bin", m.cfg.Bin), zap.String("base_url", m.BaseURL()))

	m.reclaimStaleServe(password)

	m.wg.Add(2)
	go m.supervise()
	go m.probe()
}

// healthyWait returns the channel Start blocks on: the live healthy channel,
// or a closed one when the manager is stopped or was never started, so
// waiters fall through instead of hanging on a dead manager.
func (m *ProcessManager) healthyWait() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.healthy != nil {
		return m.healthy
	}
	closed := make(chan struct{})
	close(closed)
	return closed
}

// reclaimStaleServe runs before the supervisor starts and clears anything
// already listening on the port. Without it, a leftover serve from a dead
// bedrock run (or any foreign listener) holds the port and every spawn
// crashes on bind, producing an endless crash-restart loop while the health
// probe reports healthy against the leftover. A leftover is identified by
// answering /global/health with the persisted password; it is stopped just
// the same (session state survives serve restarts), only the log differs.
func (m *ProcessManager) reclaimStaleServe(password string) {
	if !portAccepts(m.cfg.Port, 3*time.Second) {
		return // port free: nothing to reclaim
	}
	pids, err := listenersOnPort(m.cfg.Port)
	if err != nil || len(pids) == 0 {
		// Busy but no pid: a foreign listener hidden by permissions (lsof
		// needs ownership), or a dead one racing us. Nothing to signal —
		// wait, and let the spawn retry with backoff if it survives.
		m.log.Warn("harness serve port busy; cannot identify listener",
			zap.Int("port", m.cfg.Port), zap.Error(err))
		waitPortFree(m.cfg.Port, 10*time.Second)
		return
	}
	client := oc.NewClient(oc.Config{BaseURL: m.BaseURL(), Password: password})
	for _, pid := range pids {
		proc, findErr := os.FindProcess(pid)
		if findErr != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		_, probeErr := client.Health(ctx)
		cancel()
		if probeErr == nil {
			m.log.Warn("harness serve port busy; stopping leftover serve from a previous run",
				zap.Int("pid", pid), zap.Int("port", m.cfg.Port))
		} else {
			m.log.Warn("harness serve port busy; stopping foreign process",
				zap.Int("pid", pid), zap.Int("port", m.cfg.Port))
		}
		stopProcess(proc)
	}
	waitPortFree(m.cfg.Port, 10*time.Second)
}

// portAccepts reports whether anything is accepting connections on the port.
func portAccepts(port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(bindHost, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// waitPortFree polls until the port stops accepting or the timeout elapses.
func waitPortFree(port int, timeout time.Duration) bool {
	addr := net.JoinHostPort(bindHost, strconv.Itoa(port))
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err != nil {
			return true
		}
		conn.Close()
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Stop terminates the serve process (SIGTERM, then SIGKILL after the grace
// period) and waits for the supervisor goroutines. Closing healthy here lets
// Start waiters return instead of blocking on a manager that will never
// become healthy (markHealthy does the same close-and-nil, so no double
// close: whoever sees it non-nil closes and nils it under the mutex).
func (m *ProcessManager) Stop() {
	m.stopOnce.Do(func() {
		m.mu.Lock()
		stopCh := m.stopCh
		if m.healthy != nil {
			close(m.healthy)
			m.healthy = nil
		}
		m.mu.Unlock()
		if stopCh != nil {
			close(stopCh)
		}
	})
	m.wg.Wait()
}

// serveEnv builds the serve process environment with a completed PATH (login
// tool dirs included) and resolves a bare bin name against it. exec.Command
// looks up bare names via the server's own PATH, which systemd keeps minimal —
// the backend must be findable regardless of who started the server.
func serveEnv(bin, password string) ([]string, string) {
	lookup := map[string]string{}
	for _, e := range os.Environ() {
		if k, v, ok := strings.Cut(e, "="); ok {
			lookup[k] = v
		}
	}
	pkg.EnsurePATH(lookup, pkg.ResolveHomeDir())
	env := make([]string, 0, len(lookup)+1)
	for k, v := range lookup {
		env = append(env, k+"="+v)
	}
	if !strings.ContainsAny(bin, `/\`) {
		for _, dir := range filepath.SplitList(lookup["PATH"]) {
			candidate := filepath.Join(dir, bin)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				bin = candidate
				break
			}
		}
	}
	return append(env, "OPENCODE_SERVER_PASSWORD="+password), bin
}

// supervise keeps a serve process running: spawn, wait for exit, restart.
// Crashes double the restart delay up to maxRestartDelay; a process that
// stayed up resets the backoff.
func (m *ProcessManager) supervise() {
	defer m.wg.Done()
	delay := m.cfg.restartDelay()
	for {
		env, bin := serveEnv(m.cfg.Bin, m.Password())
		cmd := exec.Command(bin, "serve", "--hostname", bindHost, "--port", strconv.Itoa(m.cfg.Port))
		cmd.Env = env
		configureServeProc(cmd)
		cmd.Stdout = m.serveOutputWriter()
		cmd.Stderr = m.serveOutputWriter()
		started := time.Now()
		if err := cmd.Start(); err != nil {
			m.setState(StatusDegraded, fmt.Sprintf("starting %s: %v", bin, err))
			m.log.Warn("harness serve spawn failed; retrying",
				zap.String("bin", bin), zap.Error(err), zap.Duration("retry_in", delay))
			if !m.sleepRestartDelay(delay) {
				return
			}
			delay = min(delay*2, maxRestartDelay)
			continue
		}
		m.mu.Lock()
		m.kill = func() { killServeProc(cmd) }
		m.mu.Unlock()
		m.log.Info("harness serve started", zap.Int("pid", cmd.Process.Pid))

		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		var waitErr error
		select {
		case waitErr = <-exited:
		case <-m.stopCh:
			terminateServeProc(cmd, exited)
			m.log.Info("harness serve stopped")
			return
		}
		m.rememberCrash(waitErr)
		fast := time.Since(started) < time.Minute
		if !m.sleepRestartDelay(delay) {
			return
		}
		if fast {
			delay = min(delay*2, maxRestartDelay)
		} else {
			delay = m.cfg.restartDelay()
		}
	}
}

// rememberCrash records an unexpected exit (restart counter + degraded).
func (m *ProcessManager) rememberCrash(waitErr error) {
	m.mu.Lock()
	m.restarts++
	m.kill = nil
	n := m.restarts
	m.mu.Unlock()
	reason := "exit"
	if waitErr != nil {
		reason = waitErr.Error()
	}
	m.log.Warn("harness serve exited unexpectedly; restarting", zap.Int("restarts", n), zap.String("error", reason))
	m.setState(StatusDegraded, "serve exited: "+reason)
}

// probe polls /global/health with the persisted password. Consecutive
// failures degrade the status; a hung live process is killed for restart.
func (m *ProcessManager) probe() {
	defer m.wg.Done()
	client := oc.NewClient(oc.Config{BaseURL: m.BaseURL(), Password: m.Password()})
	failures := 0
	ticker := time.NewTicker(m.cfg.healthInterval())
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		_, err := client.Health(ctx)
		cancel()
		if err == nil {
			failures = 0
			m.setState(StatusOK, "")
			m.markHealthy()
		} else {
			failures++
			m.setState(StatusDegraded, fmt.Sprintf("health probe: %v", err))
			if failures == hungKillThreshold {
				m.mu.Lock()
				kill := m.kill
				m.mu.Unlock()
				if kill != nil {
					m.log.Warn("harness serve unresponsive; killing for restart", zap.Int("failures", failures))
					kill()
				}
			}
		}
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
		}
	}
}

// markHealthy unblocks Start exactly once on the first successful probe.
func (m *ProcessManager) markHealthy() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.healthy != nil {
		close(m.healthy)
		m.healthy = nil
	}
}

func (m *ProcessManager) sleepRestartDelay(delay time.Duration) bool {
	select {
	case <-time.After(delay):
		return true
	case <-m.stopCh:
		return false
	}
}

// serveOutputWriter routes serve stdout/stderr lines into the logger. It is
// called synchronously by the exec copy goroutines, so it owns no goroutines
// and cannot outlive the process.
func (m *ProcessManager) serveOutputWriter() io.Writer {
	return &lineLogWriter{log: m.log}
}

// lineLogWriter logs each completed line separately. Output that never
// reaches a newline is flushed in maxLineChunk-sized pieces so a runaway
// subprocess cannot grow the buffer unboundedly.
type lineLogWriter struct {
	log *zap.Logger
	buf []byte
}

const maxLineChunk = 64 * 1024

func (w *lineLogWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			if len(w.buf) >= maxLineChunk {
				w.log.Debug("harness serve output", zap.String("line", string(w.buf[:maxLineChunk])))
				w.buf = w.buf[maxLineChunk:]
			}
			return len(p), nil
		}
		w.log.Debug("harness serve output", zap.String("line", string(w.buf[:i])))
		w.buf = w.buf[i+1:]
	}
}

// terminateServeProc signals the process group, then force-kills after the
// grace period if it has not exited.
func terminateServeProc(cmd *exec.Cmd, exited <-chan error) {
	signalServeProc(cmd)
	select {
	case <-exited:
	case <-time.After(shutdownGrace):
		killServeProc(cmd)
		<-exited
	}
}

// loadOrCreatePassword returns the persisted serve password, generating and
// persisting a random one on first use (0600, parent dir 0700).
func loadOrCreatePassword(file string) (string, error) {
	if raw, err := os.ReadFile(file); err == nil {
		if password := strings.TrimSpace(string(raw)); password != "" {
			return password, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("reading %s: %w", file, err)
	}
	buf := make([]byte, passwordBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating password: %w", err)
	}
	password := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return "", fmt.Errorf("creating password dir: %w", err)
	}
	if err := os.WriteFile(file, []byte(password), 0o600); err != nil {
		return "", fmt.Errorf("writing %s: %w", file, err)
	}
	return password, nil
}
