// Package bedctl implements the bedctl installer/updater CLI: installing and
// updating Bedrock Server and Deploy Agent from GitHub releases, managing the
// backing systemd/nohup services, and remembering install state on disk.
package bedctl

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Repo is the GitHub slug releases are downloaded from.
const Repo = "cabinet-fe/bedrock"

// DefaultReleaseBase is the GitHub releases base; override with BEDROCK_RELEASE_BASE for tests.
const DefaultReleaseBase = "https://github.com/" + Repo

// ReleaseBase returns the effective release base, honoring the
// BEDROCK_RELEASE_BASE override (test/mirror setups), like the shell installer.
func ReleaseBase() string {
	if base := os.Getenv("BEDROCK_RELEASE_BASE"); base != "" {
		return base
	}
	return DefaultReleaseBase
}

// Builtins lists public gh prefix mirrors tried when direct access fails.
var Builtins = []string{"https://gh-proxy.com/", "https://ghfast.top/"}

// Component is a manageable install target.
type Component string

const (
	Server Component = "server"
	Agent  Component = "agent"
)

// BinaryName returns the on-disk binary file name for a component.
func (c Component) BinaryName() string {
	if c == Agent {
		return "bedrock-agent"
	}
	return "bedrock"
}

// AssetName returns the release asset file name for a component on linux/arch.
func (c Component) AssetName(suffix string) string {
	if c == Agent {
		return "bedrock-agent-" + suffix
	}
	return "bedrock-" + suffix
}

// ConfigName returns the config file name for a component inside the install dir.
func (c Component) ConfigName() string {
	if c == Agent {
		return "bedrock-agent.yaml"
	}
	return "config.yaml"
}

// ServiceName returns the systemd unit name for a component.
func (c Component) ServiceName() string {
	if c == Agent {
		return "bedrock-agent"
	}
	return "bedrock"
}

// StopTimeoutSec mirrors systemd TimeoutStopSec: the server shuts down
// gracefully in up to 30s, the agent exits almost immediately.
func (c Component) StopTimeoutSec() int {
	if c == Agent {
		return 10
	}
	return 45
}

// HealthTries is the readiness wait budget in seconds used after start.
func (c Component) HealthTries() int {
	if c == Agent {
		return 30
	}
	return 60
}

// Valid reports whether s is a known component name.
func Valid(s string) bool {
	return s == string(Server) || s == string(Agent)
}

// ---------------------------------------------------------------- output ---

var colorEnabled = os.Getenv("NO_COLOR") == "" && isTTY(os.Stdout)

func isTTY(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func paint(code, s string) string {
	if !colorEnabled {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

// Info prints a green "==>" progress line.
func Info(format string, a ...any) {
	fmt.Printf("%s %s\n", paint("32", "==>"), fmt.Sprintf(format, a...))
}

// Warn prints a yellow warning to stderr.
func Warn(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", paint("33", "警告:"), fmt.Sprintf(format, a...))
}

// Err prints a red error to stderr.
func Err(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", paint("31", "错误:"), fmt.Sprintf(format, a...))
}

// Bold returns s wrapped in the bold color when colors are on.
func Bold(s string) string { return paint("1", s) }

// Highlight returns s wrapped in green when colors are on.
func Highlight(s string) string { return paint("32", s) }

// Code returns a raw ANSI escape (e.g. "33" for yellow, "0" for reset) or
// the empty string when colors are off, for coloring string fragments.
func Code(code string) string {
	if !colorEnabled {
		return ""
	}
	return "\033[" + code + "m"
}

// ErrSilent signals exit code 1 without printing an extra error line —
// doctor already printed per-check details.
var ErrSilent = errors.New("doctor found issues")

// PlainPath is os.PathListSeparator as a string, for composing PATH values.
func PlainPath(dirs []string) string { return strings.Join(dirs, string(os.PathListSeparator)) }
