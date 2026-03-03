package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"os/signal"
	"syscall"
	"time"

	"appcenter-agent-linux/internal/api"
	"appcenter-agent-linux/internal/config"
	"appcenter-agent-linux/internal/state"
	"appcenter-agent-linux/internal/system"
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

	st, err := state.Load(cfg.Paths.StateFile)
	if err != nil {
		panic(err)
	}
	if st.UUID == "" {
		st.UUID = cfg.Agent.UUID
	}
	if st.UUID == "" {
		st.UUID = newUUIDLike()
	}
	if st.SecretKey == "" {
		st.SecretKey = cfg.Agent.SecretKey
	}
	if err := state.Save(cfg.Paths.StateFile, st); err != nil {
		panic(err)
	}

	client := api.NewClient(cfg.Server.URL)
	info := system.CollectHostInfo()

	reg, err := client.Register(ctx, api.RegisterRequest{
		UUID:          st.UUID,
		Hostname:      info.Hostname,
		OSVersion:     info.OSVersion,
		Platform:      info.Platform,
		Arch:          info.Arch,
		Distro:        info.Distro,
		DistroVersion: info.DistroVersion,
		AgentVersion:  cfg.Agent.Version,
		CPUModel:      info.CPUModel,
		RAMGB:         info.RAMGB,
		DiskFreeGB:    info.DiskFreeGB,
	})
	if err != nil {
		logger.Printf("register failed (will retry in heartbeat loop): %v", err)
	} else {
		st.SecretKey = reg.SecretKey
		_ = state.Save(cfg.Paths.StateFile, st)
		logger.Printf("register ok: uuid=%s", st.UUID)
	}

	interval := cfg.Heartbeat.IntervalSec
	if interval <= 0 {
		interval = 60
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Println("linux agent stopping")
			return
		case <-ticker.C:
			info = system.CollectHostInfo()
			if st.SecretKey == "" {
				reg, err = client.Register(ctx, api.RegisterRequest{
					UUID:          st.UUID,
					Hostname:      info.Hostname,
					OSVersion:     info.OSVersion,
					Platform:      info.Platform,
					Arch:          info.Arch,
					Distro:        info.Distro,
					DistroVersion: info.DistroVersion,
					AgentVersion:  cfg.Agent.Version,
					CPUModel:      info.CPUModel,
					RAMGB:         info.RAMGB,
					DiskFreeGB:    info.DiskFreeGB,
				})
				if err != nil {
					logger.Printf("register retry failed: %v", err)
					continue
				}
				st.SecretKey = reg.SecretKey
				_ = state.Save(cfg.Paths.StateFile, st)
			}
			hb, hbErr := client.Heartbeat(ctx, st.UUID, st.SecretKey, api.HeartbeatRequest{
				Hostname:      info.Hostname,
				IPAddress:     info.IPAddress,
				OSUser:        system.CurrentOSUser(),
				AgentVersion:  cfg.Agent.Version,
				DiskFreeGB:    info.DiskFreeGB,
				CurrentStatus: "Idle",
				AppsChanged:   false,
				InstalledApps: []any{},
				Platform:      info.Platform,
			})
			if hbErr != nil {
				logger.Printf("heartbeat failed: %v", hbErr)
				continue
			}
			logger.Printf("heartbeat ok: status=%s", hb.Status)
			if hb.Config.HeartbeatIntervalSec > 0 && hb.Config.HeartbeatIntervalSec != interval {
				interval = hb.Config.HeartbeatIntervalSec
				ticker.Reset(time.Duration(interval) * time.Second)
				logger.Printf("heartbeat interval updated: %ds", interval)
			}
		}
	}
}

func newUUIDLike() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	// RFC4122 v4 style bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
