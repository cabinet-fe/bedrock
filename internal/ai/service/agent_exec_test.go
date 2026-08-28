package service

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"bedrock/internal/ai/model"
)

func TestTriggerLabel(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{model.TriggerManual, "手动"},
		{model.TriggerDocsGen, "文档生成"},
		{model.TriggerCron, "定时"},
		{"", "未知"},
		{"custom", "custom"},
	}
	for _, tt := range tests {
		if got := triggerLabel(tt.in); got != tt.want {
			t.Fatalf("triggerLabel(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestWriteRunIntroIsChineseAndCompact(t *testing.T) {
	agent := &model.AiAgent{Name: "文档生成", CliKey: "reasonix"}
	run := &model.AgentRun{TriggerType: model.TriggerManual}
	var lines []string
	writeRunIntro(func(s string) { lines = append(lines, s) }, agent, run,
		"/tmp/agent", "/tmp/agent/output", "/usr/bin/reasonix", 1, 10, 10*time.Minute)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"智能体「文档生成」开始执行（reasonix，手动）",
		"工作目录: /tmp/agent",
		"产出目录: /tmp/agent/output",
		"Skill 1 个，仓库 10 个，超时 600 秒，摘要输出",
		"CLI: /usr/bin/reasonix",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	for _, bad := range []string{"injected skills", "bound repo dirs", "stream-output", "full-permission", "file already closed"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("verbose leftover %q in:\n%s", bad, joined)
		}
	}
	if len(lines) > 6 {
		t.Fatalf("intro too long: %d lines\n%s", len(lines), joined)
	}
}

func TestFormatCLIFailure(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		time.Sleep(time.Millisecond)
		got := formatCLIFailure(errors.New("signal: killed"), ctx)
		if got != "执行超时，进程已被终止" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("killed", func(t *testing.T) {
		got := formatCLIFailure(errors.New("signal: killed"), context.Background())
		if !strings.Contains(got, "进程被系统杀死") {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("plain", func(t *testing.T) {
		got := formatCLIFailure(errors.New("exit status 2"), context.Background())
		if got != "执行失败: exit status 2" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestDrainExecStreamIgnoresClosedPipe(t *testing.T) {
	r, w := io.Pipe()
	_ = w.Close()
	_ = r.Close()
	var buf strings.Builder
	var logs []string
	drainExecStream(r, &buf, func(s string) { logs = append(logs, s) })
	for _, line := range logs {
		if strings.Contains(line, "读取输出失败") || strings.Contains(line, "stream read") {
			t.Fatalf("closed pipe should be silent, got %q", logs)
		}
	}
}

func TestDrainExecStreamForwardsLinesInRealTime(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	var buf strings.Builder
	got := make(chan string, 2)
	go drainExecStream(r, &buf, func(s string) { got <- s })

	if _, err := io.WriteString(w, "first line\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-got:
		if line != "first line" {
			t.Fatalf("got %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first line not forwarded before stream end")
	}

	if _, err := io.WriteString(w, "second line\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-got:
		if line != "second line" {
			t.Fatalf("got %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second line not forwarded")
	}
	_ = w.Close()

	joined := buf.String()
	if !strings.Contains(joined, "first line") || !strings.Contains(joined, "second line") {
		t.Fatalf("buf=%q", joined)
	}
}

func TestIsBenignPipeClose(t *testing.T) {
	if !isBenignPipeClose(io.EOF) || !isBenignPipeClose(os.ErrClosed) || !isBenignPipeClose(io.ErrClosedPipe) {
		t.Fatal("EOF/ErrClosed/ErrClosedPipe should be benign")
	}
	if !isBenignPipeClose(errors.New("read |0: file already closed")) {
		t.Fatal("file already closed should be benign")
	}
	if isBenignPipeClose(errors.New("connection reset")) {
		t.Fatal("unrelated error should not be benign")
	}
}

func TestRunAgentCLICapturesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/echo")
	}
	var logs []string
	out, err := runAgentCLI(context.Background(), "/bin/echo", []string{"hello"}, "", nil, func(s string) {
		logs = append(logs, s)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("out=%q logs=%q", out, logs)
	}
}

func TestRunAgentCLITimeoutDoesNotLogClosedPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sleep")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var logs []string
	_, err := runAgentCLI(ctx, "/bin/sleep", []string{"5"}, "", nil, func(s string) {
		logs = append(logs, s)
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	joined := strings.Join(logs, "\n")
	if strings.Contains(joined, "file already closed") || strings.Contains(joined, "读取输出失败") {
		t.Fatalf("pipe close leaked into logs: %q", joined)
	}
	msg := formatCLIFailure(err, ctx)
	if !strings.Contains(msg, "超时") {
		t.Fatalf("want 超时 in %q (err=%v)", msg, err)
	}
}

func TestFormatCLIFailureExitKilled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal: killed is POSIX")
	}
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Process.Kill()
	err := cmd.Wait()
	if err == nil {
		t.Fatal("expected killed")
	}
	got := formatCLIFailure(err, context.Background())
	if !strings.Contains(got, "进程被系统杀死") {
		t.Fatalf("got %q err=%v", got, err)
	}
}
