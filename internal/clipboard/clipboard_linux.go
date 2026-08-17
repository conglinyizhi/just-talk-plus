//go:build linux

package clipboard

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"
)

const commandTimeout = 3 * time.Second

func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func runCmd(args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return string(bytes.TrimSpace(out)), ctx.Err()
	}
	return string(bytes.TrimSpace(out)), err
}

func runCmdWithInput(ctx context.Context, args []string, input string) (string, error) {
	// Use a regular file for stdin. wl-copy forks a background owner; giving it
	// an os/exec pipe lets the child retain that pipe and make cmd.Run wait for
	// the owner lifetime instead of the short-lived parent command.
	in, err := os.CreateTemp("", "just-talk-plus-clipboard-*")
	if err != nil {
		return "", err
	}
	path := in.Name()
	defer os.Remove(path)
	defer in.Close()
	if _, err := in.WriteString(input); err != nil {
		return "", err
	}
	if _, err := in.Seek(0, 0); err != nil {
		return "", err
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return "", err
	}
	defer devNull.Close()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = in
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	return "", nil
}

func candidates() []clipCmd {
	x11 := clipCmd{
		// xclip (X11)
		get: []string{"xclip", "-o", "-selection", "clipboard"},
		set: []string{"xclip", "-selection", "clipboard"},
		// Write to both PRIMARY selection AND CUT_BUFFER0.
		// CUT_BUFFER0 is the classic X11 cut buffer that doesn't need
		// a running process — Shift+Insert reads from it directly.
		setPrimary: []string{"sh", "-c", "xclip -selection primary; xprop -root -format CUT_BUFFER0 8s -set CUT_BUFFER0 \"$(xclip -o -selection primary)\""},
	}
	wayland := clipCmd{
		// wl-clipboard (Wayland)
		get:        []string{"wl-paste"},
		set:        []string{"wl-copy", "--type", "text/plain;charset=utf-8"},
		setPrimary: []string{"wl-copy", "--primary", "--type", "text/plain;charset=utf-8"},
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		return []clipCmd{wayland}
	}
	return []clipCmd{x11, wayland}
}
