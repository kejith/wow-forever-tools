//go:build !windows

package main

// Outside Windows (tests and builds in WSL or Linux): the game runs under Windows, so its process is asked
// through powershell.exe, which WSL can start; screenshots need the Windows window.

import (
	"errors"
	"image"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func wowRunning() map[string]bool {
	out, err := exec.Command("powershell.exe", "-NoProfile", "-Command",
		`(Get-Process -Name Wow -ErrorAction SilentlyContinue | ForEach-Object { $_.Path }) -join "|"`).Output()
	if err != nil {
		return map[string]bool{"running": false, "unknown": true}
	}
	forever, other := false, false
	for _, p := range strings.Split(strings.TrimSpace(string(out)), "|") {
		if p == "" {
			continue
		}
		if strings.Contains(strings.ToLower(p), "_forever_") {
			forever = true
		} else {
			other = true
		}
	}
	return map[string]bool{"running": forever, "otherClient": !forever && other}
}

func openPath(p string) error {
	if _, err := os.Stat(p); err != nil {
		return err
	}
	return exec.Command("xdg-open", p).Start()
}

func osLocale() string {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if l := os.Getenv(v); l != "" {
			return l
		}
	}
	return ""
}

func hiddenProcess() *syscall.SysProcAttr { return nil }

func detachedProcess() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

func captureWindow() (image.Image, error) {
	return nil, errors.New("screenshots need the Windows build")
}
