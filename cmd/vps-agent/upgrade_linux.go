//go:build !windows

package main

import (
	"fmt"
	"os"
)

func activateAgentUpgrade(executable, stage, backup, _ string) error {
	_ = os.Remove(backup)
	if err := os.Rename(executable, backup); err != nil {
		return fmt.Errorf("backup current agent: %w", err)
	}
	if err := os.Rename(stage, executable); err != nil {
		_ = os.Rename(backup, executable)
		return fmt.Errorf("activate staged agent: %w", err)
	}
	return nil
}
