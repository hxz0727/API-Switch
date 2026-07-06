package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hxz0727/API-Switch/internal/proxy"
)

// runMonitor handles the monitor command.
func runMonitor(cmd *cobra.Command, args []string) error {
	// Get address from config
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	return proxy.MonitorConnect(addr)
}
