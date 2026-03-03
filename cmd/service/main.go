package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"
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

const processedTasksMaxEntries = 500

var sha256HexRe = regexp.MustCompile(`^[a-f0-9]{64}$`)

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
	taskGuard := newTaskDeduper(30 * time.Minute)
	taskGuard.Seed(st.ProcessedTasks)
	persistTaskDeduper(cfg.Paths.StateFile, st, taskGuard, logger)

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
				reportTaskStatus(ctx, client, st.UUID, st.SecretKey, cmd.TaskID, api.TaskStatusRequest{
					Status:   "failed",
					Progress: 100,
					Message:  "Unsupported command action",
					Error:    fmt.Sprintf("unsupported action: %s", strings.TrimSpace(cmd.Action)),
				}, logger)
				continue
			}
			if !taskGuard.TryStart(cmd.TaskID) {
				logger.Printf("task=%d duplicate command skipped", cmd.TaskID)
				continue
			}
			if errMsg := validateInstallCommand(cmd); errMsg != "" {
				logger.Printf("task=%d invalid install command: %s", cmd.TaskID, errMsg)
				terminalReported := true
				if cmd.TaskID > 0 {
					terminalReported = reportTaskStatus(ctx, client, st.UUID, st.SecretKey, cmd.TaskID, api.TaskStatusRequest{
						Status:   "failed",
						Progress: 100,
						Message:  "Invalid install command",
						Error:    errMsg,
					}, logger)
				}
				taskGuard.Finish(cmd.TaskID, terminalReported)
				persistTaskDeduper(cfg.Paths.StateFile, st, taskGuard, logger)
				continue
			}
			terminalReported := runInstallCommand(ctx, client, cfg, st.UUID, st.SecretKey, cmd, logger)
			taskGuard.Finish(cmd.TaskID, terminalReported)
			persistTaskDeduper(cfg.Paths.StateFile, st, taskGuard, logger)
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

func runInstallCommand(ctx context.Context, client *api.Client, cfg *config.Config, agentUUID, secret string, cmd api.Command, logger *log.Logger) bool {
	start := time.Now()
	reportTaskStatus(ctx, client, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
		Status:   "downloading",
		Progress: 10,
		Message:  "Download started",
	}, logger)

	fileName := buildDownloadFilename(cmd)
	outPath, n, err := downloadWithRetry(ctx, client, agentUUID, secret, cmd, cfg.Download.TempDir, fileName, logger)
	downloadSec := int(time.Since(start).Seconds())
	if err != nil {
		logger.Printf("task=%d download failed: %v", cmd.TaskID, err)
		return reportTaskStatus(ctx, client, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
			Status:              "failed",
			Progress:            100,
			Message:             "Download failed",
			Error:               err.Error(),
			DownloadDurationSec: downloadSec,
		}, logger)
	}
	if n > 0 {
		logger.Printf("task=%d download ok: bytes=%d path=%s", cmd.TaskID, n, outPath)
	}
	defer cleanupDownloadedPackage(outPath, cmd.TaskID, logger)
	if cfg.Download.MaxSizeBytes > 0 && n > cfg.Download.MaxSizeBytes {
		errMsg := fmt.Sprintf("download size limit exceeded: got=%d max=%d", n, cfg.Download.MaxSizeBytes)
		logger.Printf("task=%d %s", cmd.TaskID, errMsg)
		return reportTaskStatus(ctx, client, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
			Status:              "failed",
			Progress:            100,
			Message:             "Download exceeds size limit",
			Error:               errMsg,
			DownloadDurationSec: downloadSec,
		}, logger)
	}
	if cmd.FileSizeBytes > 0 && n != cmd.FileSizeBytes {
		errMsg := fmt.Sprintf("download size mismatch: got=%d expected=%d", n, cmd.FileSizeBytes)
		logger.Printf("task=%d %s", cmd.TaskID, errMsg)
		return reportTaskStatus(ctx, client, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
			Status:              "failed",
			Progress:            100,
			Message:             "Download size mismatch",
			Error:               errMsg,
			DownloadDurationSec: downloadSec,
		}, logger)
	}
	if err := utils.VerifySHA256(outPath, cmd.FileHash); err != nil {
		logger.Printf("task=%d hash verify failed: %v", cmd.TaskID, err)
		return reportTaskStatus(ctx, client, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
			Status:              "failed",
			Progress:            100,
			Message:             "Hash verification failed",
			Error:               err.Error(),
			DownloadDurationSec: downloadSec,
		}, logger)
	}

	reportTaskStatus(ctx, client, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
		Status:              "downloading",
		Progress:            80,
		Message:             "Download completed",
		DownloadDurationSec: downloadSec,
	}, logger)
	reportTaskStatus(ctx, client, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
		Status:              "downloading",
		Progress:            90,
		Message:             "Install started",
		DownloadDurationSec: downloadSec,
	}, logger)

	installStart := time.Now()
	installCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Install.TimeoutSec)*time.Second)
	defer cancel()
	stdout, exitCode, installErr := installer.Install(installCtx, outPath, cmd.InstallArgs)
	installSec := int(time.Since(installStart).Seconds())

	if installErr != nil {
		if errors.Is(installErr, context.DeadlineExceeded) || installCtx.Err() == context.DeadlineExceeded {
			msg := "Install timed out"
			if strings.TrimSpace(stdout) != "" {
				msg = msg + " | " + trimOutput(stdout, 1200)
			}
			logger.Printf("task=%d install timeout: %s", cmd.TaskID, msg)
			return reportTaskStatus(ctx, client, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
				Status:              "timeout",
				Progress:            100,
				Message:             "Install timed out",
				Error:               msg,
				DownloadDurationSec: downloadSec,
				InstallDurationSec:  installSec,
			}, logger)
		}
		msg := installErr.Error()
		if strings.TrimSpace(stdout) != "" {
			msg = msg + " | " + trimOutput(stdout, 1200)
		}
		code := exitCode
		logger.Printf("task=%d install failed: %s", cmd.TaskID, msg)
		return reportTaskStatus(ctx, client, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
			Status:              "failed",
			Progress:            100,
			Message:             "Install failed",
			Error:               msg,
			ExitCode:            &code,
			DownloadDurationSec: downloadSec,
			InstallDurationSec:  installSec,
		}, logger)
	}

	successCode := 0
	terminalReported := reportTaskStatus(ctx, client, agentUUID, secret, cmd.TaskID, api.TaskStatusRequest{
		Status:              "success",
		Progress:            100,
		Message:             "Install completed",
		ExitCode:            &successCode,
		InstalledVersion:    cmd.AppVersion,
		DownloadDurationSec: downloadSec,
		InstallDurationSec:  installSec,
	}, logger)
	logger.Printf("task=%d install success", cmd.TaskID)
	return terminalReported
}

func reportTaskStatus(ctx context.Context, client *api.Client, agentUUID, secret string, taskID int, req api.TaskStatusRequest, logger *log.Logger) bool {
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := client.ReportTaskStatus(ctx, agentUUID, secret, taskID, req)
		if err == nil {
			if attempt > 1 {
				logger.Printf("task=%d status report recovered: status=%s progress=%d attempt=%d", taskID, req.Status, req.Progress, attempt)
			}
			return true
		}
		logger.Printf("task=%d status report warning: status=%s progress=%d attempt=%d err=%v", taskID, req.Status, req.Progress, attempt, err)
		if attempt == maxAttempts {
			return false
		}
		backoff := time.Duration(attempt*300) * time.Millisecond
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}
	}
	return false
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

func cleanupDownloadedPackage(path string, taskID int, logger *log.Logger) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Printf("task=%d cleanup warning: %v", taskID, err)
	}
}

func downloadWithRetry(
	ctx context.Context,
	client *api.Client,
	agentUUID, secret string,
	cmd api.Command,
	outDir, fileName string,
	logger *log.Logger,
) (string, int64, error) {
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		outPath, n, err := client.DownloadToFile(ctx, agentUUID, secret, cmd.DownloadURL, outDir, fileName)
		if err == nil {
			if attempt > 1 {
				logger.Printf("task=%d download recovered: attempt=%d", cmd.TaskID, attempt)
			}
			return outPath, n, nil
		}
		logger.Printf("task=%d download attempt=%d failed: %v", cmd.TaskID, attempt, err)
		if attempt == maxAttempts {
			return "", 0, err
		}
		backoff := time.Duration(attempt*400) * time.Millisecond
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return "", 0, fmt.Errorf("download retry exhausted")
}

type taskDeduper struct {
	ttl           time.Duration
	retryCooldown time.Duration
	mu            sync.Mutex
	doneTime      map[int]time.Time
	inflight      map[int]time.Time
	retryAfter    map[int]time.Time
}

func newTaskDeduper(ttl time.Duration) *taskDeduper {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &taskDeduper{
		ttl:           ttl,
		retryCooldown: 30 * time.Second,
		doneTime:      make(map[int]time.Time),
		inflight:      make(map[int]time.Time),
		retryAfter:    make(map[int]time.Time),
	}
}

func (d *taskDeduper) TryStart(taskID int) bool {
	if taskID <= 0 {
		return true
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pruneLocked(now)
	if _, ok := d.inflight[taskID]; ok {
		return false
	}
	if t, ok := d.retryAfter[taskID]; ok && now.Before(t) {
		return false
	}
	if t, ok := d.doneTime[taskID]; ok && now.Sub(t) < d.ttl {
		return false
	}
	d.inflight[taskID] = now
	return true
}

func (d *taskDeduper) Finish(taskID int, markDone bool) {
	if taskID <= 0 {
		return
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.inflight, taskID)
	if markDone {
		d.doneTime[taskID] = now
		delete(d.retryAfter, taskID)
	} else {
		d.retryAfter[taskID] = now.Add(d.retryCooldown)
	}
	d.pruneLocked(now)
}

func (d *taskDeduper) pruneLocked(now time.Time) {
	for id, t := range d.doneTime {
		if now.Sub(t) >= d.ttl {
			delete(d.doneTime, id)
		}
	}
	for id, t := range d.inflight {
		if now.Sub(t) >= d.ttl {
			delete(d.inflight, id)
		}
	}
	for id, t := range d.retryAfter {
		if !now.Before(t) {
			delete(d.retryAfter, id)
		}
	}
}

func (d *taskDeduper) Seed(items []state.ProcessedTask) {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, it := range items {
		if it.TaskID <= 0 || it.ExecutedAtUnix <= 0 {
			continue
		}
		t := time.Unix(it.ExecutedAtUnix, 0)
		if now.Sub(t) >= d.ttl {
			continue
		}
		d.doneTime[it.TaskID] = t
	}
	d.pruneLocked(now)
}

func (d *taskDeduper) Snapshot() []state.ProcessedTask {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pruneLocked(now)
	out := make([]state.ProcessedTask, 0, len(d.doneTime))
	for id, t := range d.doneTime {
		out = append(out, state.ProcessedTask{
			TaskID:         id,
			ExecutedAtUnix: t.Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ExecutedAtUnix > out[j].ExecutedAtUnix
	})
	if len(out) > processedTasksMaxEntries {
		out = out[:processedTasksMaxEntries]
	}
	return out
}

func persistTaskDeduper(path string, st *state.AgentState, guard *taskDeduper, logger *log.Logger) {
	if st == nil || guard == nil {
		return
	}
	st.ProcessedTasks = guard.Snapshot()
	if err := state.Save(path, st); err != nil {
		logger.Printf("state save warning: %v", err)
	}
}

func validateInstallCommand(cmd api.Command) string {
	if cmd.TaskID <= 0 {
		return "task_id must be positive"
	}
	if cmd.AppID <= 0 {
		return "app_id must be positive"
	}
	if strings.TrimSpace(cmd.DownloadURL) == "" {
		return "download_url is required"
	}
	if strings.TrimSpace(cmd.FileHash) == "" {
		return "file_hash is required"
	}
	if cmd.FileSizeBytes < 0 {
		return "file_size_bytes must be >= 0"
	}
	hash := strings.ToLower(strings.TrimSpace(cmd.FileHash))
	hash = strings.TrimPrefix(hash, "sha256:")
	if !sha256HexRe.MatchString(hash) {
		return "file_hash must be sha256 hex"
	}
	return ""
}
