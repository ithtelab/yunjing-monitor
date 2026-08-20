//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func activateAgentUpgrade(executable, stage, backup, pendingPath string) error {
	for _, value := range []string{executable, stage, backup, pendingPath} {
		if strings.ContainsAny(value, "\r\n\"") {
			return fmt.Errorf("unsupported agent path")
		}
	}
	helper := executable + ".upgrade.cmd"
	script := "@echo off\r\n" +
		"ping 127.0.0.1 -n 4 >nul\r\n" +
		"del /f /q \"" + backup + "\" >nul 2>&1\r\n" +
		"move /y \"" + executable + "\" \"" + backup + "\" >nul\r\n" +
		"if errorlevel 1 (del /f /q \"" + pendingPath + "\" >nul 2>&1 & sc start vps-agent >nul 2>&1 & exit /b 1)\r\n" +
		"move /y \"" + stage + "\" \"" + executable + "\" >nul\r\n" +
		"if errorlevel 1 (move /y \"" + backup + "\" \"" + executable + "\" >nul & del /f /q \"" + pendingPath + "\" >nul 2>&1 & sc start vps-agent >nul 2>&1 & exit /b 1)\r\n" +
		"sc start vps-agent >nul 2>&1\r\n" +
		"del /f /q \"%~f0\"\r\n"
	if err := os.WriteFile(helper, []byte(script), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("cmd.exe", "/C", helper)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008 | 0x00000200}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(helper)
		return err
	}
	return nil
}
