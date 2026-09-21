package bedctl

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// NormalizeMirror canonicalizes a mirror prefix: scheme added when missing,
// trailing slash appended; empty stays empty (direct access).
func NormalizeMirror(m string) string {
	m = strings.TrimSpace(m)
	if m == "" {
		return ""
	}
	if !strings.HasPrefix(m, "http://") && !strings.HasPrefix(m, "https://") {
		m = "https://" + m
	}
	if !strings.HasSuffix(m, "/") {
		m += "/"
	}
	return m
}

// MirrorLabel renders a mirror prefix for display.
func MirrorLabel(mirror string) string {
	if mirror == "" {
		return "直连 GitHub"
	}
	return mirror
}

// MirrorSource picks the download mirror prefix ("" means direct) for
// release URLs. Selection order — fixing the shell version's habit of
// re-asking on every run:
//
//  1. --mirror flag / BEDROCK_MIRROR env (normalized, persisted)
//  2. --no-mirror: direct, persisted (clears a stale saved mirror)
//  3. saved MIRROR from bedctl.env when it still probes OK — no prompting
//  4. probe direct then builtins; interactive runs prompt with the winner
//     as default, non-interactive runs take the winner (or the saved value
//     when nothing probes OK)
//
// The decision is persisted, and Remember records a mirror that actually
// served a download so later runs skip probing entirely.
type MirrorSource struct {
	Base         string        // release base URL
	Builtins     []string      // builtin prefix mirrors
	Client       *http.Client  // honors HTTPS_PROXY/HTTP_PROXY/NO_PROXY automatically
	ProbeTimeout time.Duration // per probe, default 5s
	State        *State
	FlagMirror   string // raw --mirror value (may be empty)
	FlagNoMirror bool
	Interactive  bool
	// PromptMirrors, when interactive, renders the mirror menu and returns
	// the chosen prefix ("" = direct GitHub) or an error (e.g. nothing
	// entered for a custom URL). def is the probed default for display:
	// 1 = direct, 2 = first builtin.
	PromptMirrors func(builtins []string, def int) (mirror string, err error)
	selected      string
}

// Selected returns the mirror prefix resolved by Pick ("" = direct).
func (m *MirrorSource) Selected() string { return m.selected }

// Pick resolves the mirror prefix and persists the decision.
func (m *MirrorSource) Pick(ctx context.Context) error {
	builtins := m.Builtins
	if builtins == nil {
		builtins = Builtins
	}
	client := m.Client
	if client == nil {
		client = http.DefaultClient
	}
	timeout := m.ProbeTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	// 1/2: an explicit choice wins and is persisted either way.
	if flag := NormalizeMirror(m.FlagMirror); flag != "" {
		Info("使用指定下载源: %s", flag)
		m.selected = flag
		m.persist(flag)
		return nil
	}
	if m.FlagNoMirror {
		Info("使用下载源: 直连 GitHub")
		m.selected = ""
		m.persist("")
		return nil
	}

	probe := func(prefix string) bool {
		url := m.Base + "/releases"
		if prefix != "" {
			url = prefix + url
		}
		return m.probeURL(ctx, client, url, timeout)
	}

	// 3: a saved mirror that still probes OK is used silently.
	if saved, ok := m.State.Get(KeyMirror); ok && saved != "" {
		if probe(saved) {
			Info("使用已保存的下载源: %s", saved)
			m.selected = saved
			return nil
		}
		Warn("已保存的下载源不可达: %s", saved)
	}

	// 4: probe direct, then builtins.
	directOK := probe("")
	winner := ""
	if !directOK {
		for _, b := range builtins {
			if probe(b) {
				winner = b
				break
			}
		}
	}

	var mirror string
	switch {
	case !m.Interactive:
		if directOK || winner != "" {
			mirror = winner
		} else if saved, ok := m.State.Get(KeyMirror); ok && saved != "" {
			// Nothing reachable: fall back to the saved mirror as last resort.
			Warn("直连与内置镜像均不可达，按上次记录的下载源继续: %s", saved)
			mirror = saved
		} else {
			Warn("GitHub 与内置镜像均不可达，将按直连继续；失败时可重试并指定 --mirror <URL>")
		}
	default:
		def := 1
		if winner != "" {
			def = 2
		}
		if m.PromptMirrors != nil {
			chosen, err := m.PromptMirrors(builtins, def)
			if err != nil {
				return err
			}
			mirror = chosen
		}
		if !directOK && winner == "" && mirror == "" {
			Warn("GitHub 与内置镜像均不可达，将按直连继续；失败时可重试并指定 --mirror <URL>")
		}
	}

	Info("使用下载源: %s", MirrorLabel(mirror))
	m.selected = mirror
	m.persist(mirror)
	return nil
}

// persist records the decision. Choosing direct writes an empty MIRROR line
// so a stale mirror saved by an earlier run can never resurface.
func (m *MirrorSource) persist(mirror string) {
	if err := m.State.Set(KeyMirror, mirror); err != nil {
		Warn("无法记录下载源到 %s: %v", m.State.Path, err)
	}
}

// Remember stores a mirror that proved working during a download so the
// next run starts from it instead of re-probing.
func (m *MirrorSource) Remember(mirror string) {
	m.persist(mirror)
}

func (m *MirrorSource) probeURL(ctx context.Context, client *http.Client, url string, timeout time.Duration) bool {
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(pctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}
