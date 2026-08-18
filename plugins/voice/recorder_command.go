//go:build linux

package voice

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

type candidate struct {
	name string
	args []string
}

func firstFound(candidates []candidate) (*exec.Cmd, string, error) {
	for _, c := range candidates {
		if path, err := exec.LookPath(c.name); err == nil {
			return exec.Command(path, c.args...), c.name, nil
		}
	}
	return nil, "", errNoBackend(candidates)
}

func startCaptureWithDevice(logger *slog.Logger, device string) (io.ReadCloser, string, func() error, error) {
	return startCommandCapture(logger, device)
}

func startCommandCapture(logger *slog.Logger, device string) (io.ReadCloser, string, func() error, error) {
	cmd, name, err := pickCommandWithDevice(device)
	if err != nil {
		logger.Error("recorder command selection failed", "device", device, "error", err)
		return nil, "", nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("recorder stdout pipe failed", "backend", name, "command", strings.Join(cmd.Args, " "), "error", err)
		return nil, "", nil, err
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		logger.Error("recorder start failed", "backend", name, "error", err, "stderr", stderrBuf.String())
		return nil, "", nil, err
	}
	pid := cmd.Process.Pid
	stop := func() error {
		if cmd.Process == nil {
			return cmd.Wait()
		}
		// 先让 arecord/parecord 正常结束，以便其将音频服务中已经抵达的
		// 尾部缓冲写入 stdout。极少数命令不响应时再用 Kill 兜底，避免停止
		// 流程无限等待。
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			logger.Warn("recorder interrupt failed", "backend", name, "pid", pid, "error", err)
		}
		waitDone := make(chan error, 1)
		go func() {
			waitErr := cmd.Wait()
			stderr := strings.TrimSpace(stderrBuf.String())
			if waitErr != nil {
				logger.Warn("recorder process stopped with status", "backend", name, "pid", pid, "command", strings.Join(cmd.Args, " "), "error", waitErr)
			} else {
				logger.Info("recorder process exited", "backend", name, "pid", pid, "command", strings.Join(cmd.Args, " "))
			}
			if stderr != "" {
				logger.Warn("recorder process stderr", "backend", name, "pid", pid, "stderr", stderr)
			}
			waitDone <- waitErr
		}()
		select {
		case err := <-waitDone:
			return err
		case <-time.After(2 * time.Second):
			if err := cmd.Process.Kill(); err != nil {
				logger.Warn("recorder kill failed", "backend", name, "pid", pid, "error", err)
			}
			return <-waitDone
		}
	}
	logger.Info("recorder process started", "backend", name, "pid", pid, "command", strings.Join(cmd.Args, " "))
	return stdout, name, stop, nil
}
