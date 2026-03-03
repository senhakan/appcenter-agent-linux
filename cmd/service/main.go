package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"appcenter-agent-linux/internal/api"
	"appcenter-agent-linux/internal/config"
	"appcenter-agent-linux/internal/installer"
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
	triggerCh := make(chan struct{}, 1)
	var hasSecret atomic.Bool
	hasSecret.Store(st.SecretKey != "")

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
		hasSecret.Store(true)
		logger.Printf("register ok: uuid=%s", st.UUID)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if !hasSecret.Load() {
				time.Sleep(2 * time.Second)
				continue
			}
			resp, sigErr := client.WaitForSignal(ctx, st.UUID, st.SecretKey, 55)
			if sigErr != nil {
				logger.Printf("signal wait failed: %v", sigErr)
				time.Sleep(2 * time.Second)
				continue
			}
			if resp != nil && resp.Status == "signal" {
				select {
				case triggerCh <- struct{}{}:
				default:
				}
			}
		}
	}()

	interval := cfg.Heartbeat.IntervalSec
	if interval <= 0 {
		interval = 60
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	sendHeartbeat := func(reason string) {
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
				logger.Printf("%s register retry failed: %v", reason, err)
				return
			}
			st.SecretKey = reg.SecretKey
			_ = state.Save(cfg.Paths.StateFile, st)
			hasSecret.Store(true)
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
			logger.Printf("%s heartbeat failed: %v", reason, hbErr)
			return
		}
		logger.Printf("%s heartbeat ok: status=%s commands=%d", reason, hb.Status, len(hb.Commands))
		if hb.Config.HeartbeatIntervalSec > 0 && hb.Config.HeartbeatIntervalSec != interval {
			interval = hb.Config.HeartbeatIntervalSec
			ticker.Reset(time.Duration(interval) * time.Second)
			logger.Printf("heartbeat interval updated: %ds", interval)
		}
		for _, cmd := range hb.Commands {
			if strings.ToLower(strings.TrimSpace(cmd.Action)) != "install" {
				logger.Printf("task=%d unsupported action: %s", cmd.TaskID, strings.TrimSpace(cmd.Action))
				_ = client.ReportTaskStatus(ctx, st.UUID, st.SecretKey, cmd.TaskID, api.TaskStatusRequest{
					Status:   "failed",
					Progress: 100,
					Message:  "Unsupported command action",
					Error:    fmt.Sprintf("unsupported action: %s", strings.TrimSpace(cmd.Action)),
				})
				continue
			}
			runInstallCommand(ctx, client, cfg, st.UUID, st.SecretKey, cmd, logger)
		}
	}

	for {
		select {
		case <-ctx.Done():
			logger.Println("linux agent stopping")
			return
		case <-ticker.C:
			sendHeartbeat("periodic")
		case <-triggerCh:
			sendHeartbeat("signal-triggered")
			ticker.Reset(time.Duration(interval) * time.Second)
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

func runInstallCommand(ctx context.Context, client *api.Client, cfg *config.Config, agentUUID, secret string, cmd api.Command, logger *log.Logger) {
	start := time.Now()
	_ = client.ReportTaskStatus(ctx, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
		Status:   "downloading",
		Progress: 10,
		Message:  "Download started",
	})

	fileName := buildDownloadFilename(cmd)
	outPath, n, err := client.DownloadToFile(ctx, agentUUID, secret, cmd.DownloadURL, cfg.Download.TempDir, fileName)
	downloadSec := int(time.Since(start).Seconds())
	if err != nil {
		logger.Printf("task=%d download failed: %v", cmd.TaskID, err)
		_ = client.ReportTaskStatus(ctx, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
			Status:              "failed",
			Progress:            100,
			Message:             "Download failed",
			Error:               err.Error(),
			DownloadDurationSec: downloadSec,
		})
		return
	}
	if n > 0 {
		logger.Printf("task=%d download ok: bytes=%d path=%s", cmd.TaskID, n, outPath)
	}
	if err := utils.VerifySHA256(outPath, cmd.FileHash); err != nil {
		logger.Printf("task=%d hash verify failed: %v", cmd.TaskID, err)
		_ = client.ReportTaskStatus(ctx, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
			Status:              "failed",
			Progress:            100,
			Message:             "Hash verification failed",
			Error:               err.Error(),
			DownloadDurationSec: downloadSec,
		})
		return
	}

	_ = client.ReportTaskStatus(ctx, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
		Status:              "downloading",
		Progress:            80,
		Message:             "Download completed",
		DownloadDurationSec: downloadSec,
	})
	_ = client.ReportTaskStatus(ctx, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
		Status:              "downloading",
		Progress:            90,
		Message:             "Install started",
		DownloadDurationSec: downloadSec,
	})

	installStart := time.Now()
	installCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Install.TimeoutSec)*time.Second)
	defer cancel()
	stdout, exitCode, installErr := installer.Install(installCtx, outPath, cmd.InstallArgs)
	installSec := int(time.Since(installStart).Seconds())

	if installErr != nil {
		msg := installErr.Error()
		if strings.TrimSpace(stdout) != "" {
			msg = msg + " | " + trimOutput(stdout, 1200)
		}
		code := exitCode
		logger.Printf("task=%d install failed: %s", cmd.TaskID, msg)
		_ = client.ReportTaskStatus(ctx, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
			Status:              "failed",
			Progress:            100,
			Message:             "Install failed",
			Error:               msg,
			ExitCode:            &code,
			DownloadDurationSec: downloadSec,
			InstallDurationSec:  installSec,
		})
		return
	}

	successCode := 0
	_ = client.ReportTaskStatus(ctx, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
		Status:              "success",
		Progress:            100,
		Message:             "Install completed",
		ExitCode:            &successCode,
		InstalledVersion:    cmd.AppVersion,
		DownloadDurationSec: downloadSec,
		InstallDurationSec:  installSec,
	})
	if err := os.Remove(outPath); err != nil {
		logger.Printf("task=%d cleanup warning: %v", cmd.TaskID, err)
	}
	logger.Printf("task=%d install success", cmd.TaskID)
}

func buildDownloadFilename(cmd api.Command) string {
	ext := ".bin"
	u := strings.ToLower(strings.TrimSpace(cmd.DownloadURL))
	switch {
	case strings.Contains(u, ".tar.gz"):
		ext = ".tar.gz"
	case strings.Contains(u, ".deb"):
		ext = ".deb"
	case strings.Contains(u, ".sh"):
		ext = ".sh"
	}
	return fmt.Sprintf("task_%d_app_%d%s", cmd.TaskID, cmd.AppID, ext)
}

func trimOutput(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
