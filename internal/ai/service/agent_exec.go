package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"bedrock/internal/ai/model"
)

const agentCLIGraceAfterCancel = 5 * time.Second

// agentLogLineMax 单行日志上限；超长行按截断提示处理，防止撑爆日志文件/WS。
const agentLogLineMax = 16 << 20

func triggerLabel(v string) string {
	switch v {
	case model.TriggerManual:
		return "手动"
	case model.TriggerAPI:
		return "API"
	case model.TriggerCron:
		return "定时"
	case model.TriggerBuildEvent:
		return "构建事件"
	case model.TriggerDocsGen:
		return "文档生成"
	case model.TriggerPipeline:
		return "流水线"
	default:
		if strings.TrimSpace(v) == "" {
			return "未知"
		}
		return v
	}
}

func writeRunIntro(writeLog func(string), agent *model.AiAgent, run *model.AgentRun, absRoot, absOutput, binary string, skillCount, repoCount int, timeout time.Duration) {
	writeLog(fmt.Sprintf("智能体「%s」开始执行（%s，%s）", agent.Name, agent.CliKey, triggerLabel(run.TriggerType)))
	writeLog("工作目录: " + absRoot)
	writeLog("产出目录: " + absOutput)
	outputMode := "摘要输出"
	if agent.StreamOutput {
		outputMode = "流式输出"
	}
	writeLog(fmt.Sprintf("Skill %d 个，仓库 %d 个，超时 %d 秒，%s", skillCount, repoCount, int(timeout.Seconds()), outputMode))
	writeLog("CLI: " + binary)
}

func formatCLIFailure(err error, runCtx context.Context) string {
	if err == nil {
		return ""
	}
	if runCtx != nil {
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			return "执行超时，进程已被终止"
		case errors.Is(runCtx.Err(), context.Canceled):
			return "执行已取消"
		}
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		if ee.ProcessState != nil && ee.ProcessState.String() == "signal: killed" {
			return "进程被系统杀死（常见于超时或内存不足）"
		}
	}
	if strings.Contains(err.Error(), "signal: killed") {
		return "进程被系统杀死（常见于超时或内存不足）"
	}
	return "执行失败: " + err.Error()
}

func isBenignPipeClose(err error) bool {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "file already closed") || strings.Contains(msg, "use of closed file") || strings.Contains(msg, "closed pipe")
}

// drainExecStream reads an exec pipe until EOF, appends to buf, and forwards
// complete lines to logFn as they arrive (real-time, not only after exit).
func drainExecStream(r io.Reader, buf *strings.Builder, logFn func(string)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), agentLogLineMax)
	for scanner.Scan() {
		line := scanner.Text()
		buf.WriteString(line)
		buf.WriteByte('\n')
		logFn(line)
	}
	if err := scanner.Err(); err != nil && !isBenignPipeClose(err) {
		if errors.Is(err, bufio.ErrTooLong) {
			logFn("读取输出失败: 单行超过上限被截断，后续输出可能丢失")
		} else {
			logFn("读取输出失败: " + err.Error())
		}
	}
}

// runAgentCLI starts the agent binary, streams stdout/stderr, then Wait.
// Pipes are drained before Wait so CommandContext kill does not surface
// "file already closed" as a user-facing log line.
func runAgentCLI(ctx context.Context, binary string, args []string, dir string, env []string, logFn func(string)) (string, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Env = env
	configureAgentCmdProc(cmd)
	cmd.Cancel = func() error { return killAgentCmdProcess(cmd) }
	cmd.WaitDelay = agentCLIGraceAfterCancel

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	var stdoutBuf, stderrBuf strings.Builder
	var wg sync.WaitGroup
	wg.Go(func() { drainExecStream(stdout, &stdoutBuf, logFn) })
	wg.Go(func() { drainExecStream(stderr, &stderrBuf, logFn) })
	wg.Wait()
	waitErr := cmd.Wait()
	return stdoutBuf.String() + stderrBuf.String(), waitErr
}
