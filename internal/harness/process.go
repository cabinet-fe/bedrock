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
	probeTimeout          = 3 * time.Second
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
// successful health probe, ctx is done, or Stop is called. Returning an
// error leaves the supervisor running (it keeps retrying); only a password
// failure is terminal.
func (m *ProcessManager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return errors.New("harness: process manager already started")
	}
	password, err := loadOrCreatePassword(m.cfg.PasswordFile)
	if err != nil {
		return fmt.Errorf("harness serve password: %w", err)
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

	m.wg.Add(2)
	go m.supervise()
	go m.probe()

	select {
	case <-healthy:
		return nil
	case <-m.stopCh:
		return errors.New("harness: process manager stopped before serve became healthy")
	case <-ctx.Done():
		return fmt.Errorf("harness serve not healthy: %s", m.Status().LastError)
	}
}

// Stop terminates the serve process (SIGTERM, then SIGKILL after the grace
// period) and waits for the supervisor goroutines.
func (m *ProcessManager) Stop() {
	m.stopOnce.Do(func() {
		m.mu.Lock()
		stopCh := m.stopCh
		m.mu.Unlock()
		if stopCh != nil {
			close(stopCh)
		}
	})
	m.wg.Wait()
}

// supervise keeps a serve process running: spawn, wait for exit, restart.
func (m *ProcessManager) supervise() {
	defer m.wg.Done()
	for {
		cmd := exec.Command(m.cfg.Bin, "serve", "--hostname", bindHost, "--port", strconv.Itoa(m.cfg.Port))
		cmd.Env = append(os.Environ(), "OPENCODE_SERVER_PASSWORD="+m.Password())
		configureServeProc(cmd)
		cmd.Stdout = m.serveOutputWriter()
		cmd.Stderr = m.serveOutputWriter()
		if err := cmd.Start(); err != nil {
			m.setState(StatusDegraded, fmt.Sprintf("starting %s: %v", m.cfg.Bin, err))
			if !m.sleepRestartDelay() {
				return
			}
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
		if !m.sleepRestartDelay() {
			return
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

func (m *ProcessManager) sleepRestartDelay() bool {
	select {
	case <-time.After(m.cfg.restartDelay()):
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
