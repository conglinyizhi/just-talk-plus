// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

//go:build linux || darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: air-tui-entrypoint <command> [args...]")
		os.Exit(2)
	}

	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		runWithoutTerminal()
		return
	}
	defer terminal.Close()

	terminalFD := int(terminal.Fd())
	previousGroup, err := unix.IoctlGetInt(terminalFD, unix.TIOCGPGRP)
	if err != nil {
		runWithoutTerminal()
		return
	}

	command := exec.Command(os.Args[1], os.Args[2:]...)
	command.Stdin = terminal
	command.Stdout = terminal
	command.Stderr = terminal
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start TUI: %v\n", err)
		os.Exit(1)
	}

	childGroup := command.Process.Pid
	signal.Ignore(syscall.SIGTTOU)
	if err := unix.IoctlSetPointerInt(terminalFD, unix.TIOCSPGRP, childGroup); err != nil {
		fmt.Fprintf(os.Stderr, "give terminal to TUI: %v\n", err)
		_ = command.Process.Kill()
		_ = command.Wait()
		os.Exit(1)
	}
	defer unix.IoctlSetPointerInt(terminalFD, unix.TIOCSPGRP, previousGroup)

	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)

	select {
	case err := <-done:
		if err != nil {
			os.Exit(exitCode(err))
		}
	case received := <-signals:
		_ = unix.IoctlSetPointerInt(terminalFD, unix.TIOCSPGRP, previousGroup)
		_ = syscall.Kill(-childGroup, received.(syscall.Signal))
		if err := <-done; err != nil {
			os.Exit(exitCode(err))
		}
	}
}

func runWithoutTerminal() {
	command := exec.Command(os.Args[1], os.Args[2:]...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}
