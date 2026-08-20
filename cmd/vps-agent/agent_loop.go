package main

import (
	"context"
	"log"
	"time"

	"vps-agent/internal/agent"
	"vps-agent/internal/config"
	"vps-agent/internal/reporter"
)

func runAgentLoop(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	rep := reporter.New(cfg)
	collector := agent.NewCollector(cfg)
	go runProbeLoop(ctx, cfg)
	startedAt := time.Now().Unix()
	nextUpgradeCheck := time.Time{}
	log.Printf("agent started node_id=%s server=%s interval=%s", cfg.NodeID, cfg.Server, cfg.BasicInterval)
	ticker := time.NewTicker(cfg.BasicInterval)
	defer ticker.Stop()
	for {
		metrics, err := collector.Collect(ctx)
		if err != nil {
			log.Printf("collect failed: %v", err)
		} else {
			metrics.AgentVersion = version
			metrics.AgentStartedAt = startedAt
			if err := rep.Send(ctx, metrics); err != nil {
				log.Printf("report failed: %v", err)
			} else if !time.Now().Before(nextUpgradeCheck) {
				nextUpgradeCheck = time.Now().Add(5 * time.Minute)
				applied, upgradeErr := checkAgentUpgrade(ctx, cfg, configPath)
				if upgradeErr != nil {
					log.Printf("upgrade check failed: %v", upgradeErr)
				} else if applied {
					log.Print("agent upgrade staged; exiting for service restart")
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			log.Print("agent stopped")
			return nil
		case <-ticker.C:
		}
	}
}
