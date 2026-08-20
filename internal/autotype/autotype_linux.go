//go:build linux

package autotype

// #cgo LDFLAGS: -lX11 -lXtst
//
// #include <X11/Xlib.h>
// #include <X11/keysym.h>
// #include <X11/extensions/XTest.h>
// #include <stdlib.h>
// #include <unistd.h>
//
// static void xtest_shift_insert(Display *dpy) {
// 	KeyCode shift = XKeysymToKeycode(dpy, XK_Shift_L);
// 	KeyCode insert = XKeysymToKeycode(dpy, XK_Insert);
// 	if (shift == 0 || insert == 0) return;
//
// 	XTestFakeKeyEvent(dpy, shift, True, 0);
// 	XFlush(dpy);
// 	usleep(15000);
//
// 	XTestFakeKeyEvent(dpy, insert, True, 0);
// 	XFlush(dpy);
// 	usleep(30000);
//
// 	XTestFakeKeyEvent(dpy, insert, False, 0);
// 	XFlush(dpy);
// 	usleep(15000);
//
// 	XTestFakeKeyEvent(dpy, shift, False, 0);
// 	XFlush(dpy);
// }
import "C"

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/bendahl/uinput"
)

const (
	uinputDev  = "/dev/uinput"
	keyDelayMs = 15
	// commandTimeout bounds clipboard and helper subprocesses so one broken
	// Wayland/X11 command cannot hold dispatchMu indefinitely.
	commandTimeout = 3 * time.Second
)

func isWaylandSession() bool {
	return os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland"
}

func pastePlatform(text string, logger *slog.Logger) error {
	if isWaylandSession() {
		return pasteWayland(text, logger)
	}
	return pasteX11(text, logger)
}

// On X11: sets PRIMARY + CLIPBOARD via xclip, simulates Shift+Insert, restores.
func pasteX11(text string, logger *slog.Logger) error {
	orig, _ := runClipboard("xclip", "-o", "-selection", "clipboard")

	if err := pipeToCmd(text, "xclip", "-selection", "clipboard"); err != nil {
		return fmt.Errorf("set clipboard: %w", err)
	}

	primaryCmd := exec.Command("xclip", "-selection", "primary")
	primaryIn, err := primaryCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("primary pipe: %w", err)
	}
	primaryCmd.Start()
	primaryIn.Write([]byte(text))
	primaryIn.Close()

	time.Sleep(50 * time.Millisecond)
	if err := simulatePaste(); err != nil {
		primaryCmd.Process.Kill()
		primaryCmd.Wait()
		return fmt.Errorf("simulate paste: %w", err)
	}

	time.Sleep(pasteSettleDelay)
	primaryCmd.Process.Kill()
	primaryCmd.Wait()

	if orig != "" && orig != text {
		pipeToCmd(orig, "xclip", "-selection", "clipboard")
	}

	logger.Debug("autotype done", "text_len", len(text))
	return nil
}

func pasteWayland(text string, logger *slog.Logger) error {
	// 自动上屏使用前台 wl-copy，由当前调用方在目标应用完成读取后主动回收。
	// 普通 clipboard.Set 仍使用持久剪贴板，避免改变用户主动复制的语义。
	sensitiveArgs := buildWaylandCopyArgs(wlCopySupportsSensitive())
	stageStarted := time.Now()
	clipboardCmd, cancelClipboard, err := startPipeCmd(text, append([]string{"wl-copy", "--foreground"}, sensitiveArgs...)...)
	if err != nil {
		logger.Debug("wayland clipboard owner start failed", "error", err, "duration_ms", time.Since(stageStarted).Milliseconds())
		return fmt.Errorf("set Wayland clipboard: %w", err)
	}
	logger.Debug("wayland clipboard owner started", "pid", clipboardCmd.Process.Pid, "duration_ms", time.Since(stageStarted).Milliseconds())
	defer func() {
		cancelClipboard()
		waitStarted := time.Now()
		err := clipboardCmd.Wait()
		logger.Debug("wayland clipboard owner reaped", "pid", clipboardCmd.Process.Pid, "wait_error", err, "duration_ms", time.Since(waitStarted).Milliseconds())
	}()

	var primaryCmd *exec.Cmd
	var cancelPrimary context.CancelFunc
	if !isKDEPlasma() {
		primaryCmd, cancelPrimary, err = startPipeCmd(text, append([]string{"wl-copy", "--primary", "--foreground"}, sensitiveArgs...)...)
		if err != nil {
			logger.Debug("wayland primary owner start failed", "error", err)
			return fmt.Errorf("set Wayland primary selection: %w", err)
		}
		logger.Debug("wayland primary owner started", "pid", primaryCmd.Process.Pid)
		defer func() {
			cancelPrimary()
			waitStarted := time.Now()
			err := primaryCmd.Wait()
			logger.Debug("wayland primary owner reaped", "pid", primaryCmd.Process.Pid, "wait_error", err, "duration_ms", time.Since(waitStarted).Milliseconds())
		}()
	}

	logger.Debug("wayland paste simulation start")
	time.Sleep(50 * time.Millisecond)
	if err := simulatePaste(); err != nil {
		logger.Debug("wayland paste simulation failed", "error", err)
		return fmt.Errorf("simulate paste: %w", err)
	}
	logger.Debug("wayland paste simulation sent")

	// 给目标应用留出读取剪贴板的时间，随后 defer 会明确回收 wl-copy。
	time.Sleep(pasteSettleDelay)
	logger.Debug("wayland paste settle done", "duration_ms", pasteSettleDelay.Milliseconds())

	logger.Debug("autotype done", "text_len", len(text), "method", pasteMethod())
	return nil
}

// buildWaylandCopyArgs 构造 wl-copy 参数：支持 --sensitive 时以它开头，
// 并始终指定 text/plain 类型（内容本身不受影响，仅附加敏感提示）。
func buildWaylandCopyArgs(sensitive bool) []string {
	args := []string{"--type", "text/plain;charset=utf-8"}
	if sensitive {
		args = append([]string{"--sensitive"}, args...)
	}
	return args
}

// wlCopySupportsSensitive 检测 wl-copy 是否支持 --sensitive（wl-clipboard ≥2.2），结果缓存。
func wlCopySupportsSensitive() bool {
	sensitiveOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "wl-copy", "--help").Output()
		sensitiveOK = err == nil && ctx.Err() == nil && strings.Contains(string(out), "--sensitive")
	})
	return sensitiveOK
}

var (
	sensitiveOnce sync.Once
	sensitiveOK   bool
)

func runClipboard(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func pipeToCmd(input string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd, err := startPipeCmdWithContext(ctx, input, args...)
	if err != nil {
		return err
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func startPipeCmd(input string, args ...string) (*exec.Cmd, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	cmd, err := startPipeCmdWithContext(ctx, input, args...)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return cmd, cancel, nil
}

func startPipeCmdWithContext(ctx context.Context, input string, args ...string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if _, err := stdin.Write([]byte(input)); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return nil, err
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return nil, err
	}
	return cmd, nil
}

func simulatePaste() error {
	// Check for Wayland
	if isWaylandSession() {
		return simulatePasteWayland()
	}
	return simulatePasteX11()
}

func simulatePasteX11() error {
	dpy := C.XOpenDisplay(nil)
	if dpy == nil {
		return fmt.Errorf("cannot open X display")
	}
	defer C.XCloseDisplay(dpy)

	C.xtest_shift_insert(dpy)
	return nil
}

func simulatePasteWayland() error {
	if !isKDEPlasma() {
		if _, err := exec.LookPath("wtype"); err == nil {
			if err := exec.Command("wtype", "-M", "shift", "-k", "Insert", "-m", "shift").Run(); err == nil {
				return nil
			}
		}
	}
	return simulatePasteUinput()
}

func isKDEPlasma() bool {
	desktop := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP") + " " + os.Getenv("DESKTOP_SESSION"))
	return strings.Contains(desktop, "kde") || strings.Contains(desktop, "plasma") || os.Getenv("KDE_SESSION_VERSION") != ""
}

func simulatePasteUinput() error {
	keyboard, err := uinput.CreateKeyboard(uinputDev, []byte("just-talk-plus virtual keyboard"))
	if err != nil {
		return fmt.Errorf("create uinput keyboard: %w", err)
	}
	defer keyboard.Close()

	time.Sleep(80 * time.Millisecond)
	if err := keyboard.KeyDown(uinput.KeyLeftshift); err != nil {
		return fmt.Errorf("uinput shift down: %w", err)
	}
	time.Sleep(keyDelayMs * time.Millisecond)
	if err := keyboard.KeyDown(uinput.KeyInsert); err != nil {
		return fmt.Errorf("uinput insert down: %w", err)
	}
	time.Sleep(keyDelayMs * time.Millisecond)
	if err := keyboard.KeyUp(uinput.KeyInsert); err != nil {
		return fmt.Errorf("uinput insert up: %w", err)
	}
	time.Sleep(keyDelayMs * time.Millisecond)
	if err := keyboard.KeyUp(uinput.KeyLeftshift); err != nil {
		return fmt.Errorf("uinput shift up: %w", err)
	}
	return nil
}

func pasteMethod() string {
	if isWaylandSession() {
		if !isKDEPlasma() {
			if _, err := exec.LookPath("wtype"); err == nil {
				return "wayland/wtype+Shift+Insert"
			}
		}
		return "wayland/uinput+Shift+Insert"
	}
	return "x11/XTest+Shift+Insert"
}
