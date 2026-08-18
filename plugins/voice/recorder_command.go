//go:build linux

package voice

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
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
		return nil, "", nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", nil, err
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		logger.Error("recorder start failed", "backend", name, "error", err, "stderr", stderrBuf.String())
		return nil, "", nil, err
	}
	stop := func() error {
		if cmd.Process == nil {
			return cmd.Wait()
		}
		// 先让 arecord/parecord 正常结束，以便其将音频服务中已经抵达的
		// 尾部缓冲写入 stdout。极少数命令不响应时再用 Kill 兜底，避免停止
		// 流程无限等待。
		_ = cmd.Process.Signal(os.Interrupt)
		waitDone := make(chan error, 1)
		go func() { waitDone <- cmd.Wait() }()
		select {
		case err := <-waitDone:
			return err
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			return <-waitDone
		}
	}
	return stdout, name, stop, nil
}
