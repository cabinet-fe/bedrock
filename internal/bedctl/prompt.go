package bedctl

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// Prompts asks questions on /dev/tty so `curl | bash` keeps stdin for the
// installer itself. When no tty is available every question resolves to its
// default and the resolution is printed, so non-interactive runs are never
// silently surprising.
type Prompts struct {
	ttyIn  *os.File
	ttyOut io.Writer
	seen   bool // whether the no-tty notice was already printed
}

// NewPrompts opens /dev/tty; ok is false when there is no controlling
// terminal (piped runs, cron, CI).
func NewPrompts() (*Prompts, bool) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return &Prompts{ttyOut: os.Stderr}, false
	}
	return &Prompts{ttyIn: tty, ttyOut: tty}, true
}

func (p *Prompts) readLine() string {
	if p.ttyIn == nil {
		if !p.seen {
			p.seen = true
			Warn("未检测到交互终端，以下问题均采用默认值（如需自定义请用命令行参数或 --yes 明确指定）")
		}
		return ""
	}
	reader := bufio.NewReader(p.ttyIn)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimRight(line, "\r\n")
}

// Ask prints prompt with default and returns the answer (default when empty).
func (p *Prompts) Ask(prompt, def string) string {
	fmt.Fprintf(p.ttyOut, "%s [%s]: ", Bold(prompt), def)
	in := p.readLine()
	if strings.TrimSpace(in) == "" {
		in = def
	}
	if p.ttyIn == nil {
		fmt.Fprintf(p.ttyOut, "%s\n", in)
	}
	return in
}

// AskChoice loops until the answer matches valid; def is returned for empty input.
func (p *Prompts) AskChoice(prompt, valid, def string) string {
	re := regexp.MustCompile(valid)
	for {
		in := p.Ask(prompt, def)
		if re.MatchString(in) {
			return in
		}
		Warn("无效输入: %s", in)
	}
}
