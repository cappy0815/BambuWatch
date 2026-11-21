//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// ffmpeg ohne Konsolenfenster starten
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
