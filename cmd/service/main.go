package main

import (
	"context"
	"flag"
	"os/signal"
	"syscall"
	"time"

	"appcenter-agent-linux/internal/config"
	"appcenter-agent-linux/pkg/utils"
)

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", "", "config file path")
	flag.Parse()

	resolved := config.ResolvePath(cfgPath)
	cfg, err := config.Load(resolved)
	if err != nil {
		panic(err)
	}
	if err := config.EnsureDirs(cfg); err != nil {
		panic(err)
	}

	logger, closer, err := utils.NewLogger(cfg.Logging.File)
	if err != nil {
		panic(err)
	}
	defer closer.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Printf("linux agent starting: version=%s server=%s", cfg.Agent.Version, cfg.Server.URL)
	logger.Printf("linux agent runtime: ipc=%s remote_support_enabled=%t", cfg.IPC.SocketPath, cfg.RemoteSupport.Enabled)

	ticker := time.NewTicker(time.Duration(cfg.Heartbeat.IntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Println("linux agent stopping")
			return
		case <-ticker.C:
			logger.Println("heartbeat tick (bootstrap)")
		}
	}
}
