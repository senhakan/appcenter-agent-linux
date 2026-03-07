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
	"path/filepath"
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
	"appcenter-agent-linux/internal/inventory"
	"appcenter-agent-linux/internal/ipc"
	"appcenter-agent-linux/internal/remotesupport"
	svcmon "appcenter-agent-linux/internal/services"
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
	logger.Printf("linux agent install queue: capacity=%d workers=%d", cfg.Install.QueueCapacity, cfg.Install.WorkerCount)

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
	remoteDisplay := remotesupport.ResolveDisplay(cfg.RemoteSupport.Display, logger)
	remoteSupportManager := remotesupport.NewManager(logger, remoteDisplay, cfg.RemoteSupport.Port)
	remoteSupportSession := remotesupport.NewSessionManager()
	remoteSupportSession.Restore(remoteSupportSessionFromState(st.RemoteSupportSession))
	if snap := remoteSupportSession.Snapshot(); strings.TrimSpace(snap.State) != "" && strings.TrimSpace(snap.State) != remotesupport.StateIdle {
		logger.Printf("remote support session restored: state=%s session_id=%d", snap.State, snap.SessionID)
	}
	persistRemoteSupportSession := func() {
		st.RemoteSupportSession = remoteSupportSessionToState(remoteSupportSession.Snapshot())
		if err := state.Save(cfg.Paths.StateFile, st); err != nil {
			logger.Printf("remote support state save warning: %v", err)
		}
	}
	callRemoteWithRetry := func(op string, fn func(callCtx context.Context) error) error {
		const maxAttempts = 3
		var lastErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			callCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := fn(callCtx)
			cancel()
			if err == nil {
				if attempt > 1 {
					logger.Printf("remote support callback recovered: op=%s attempt=%d", op, attempt)
				}
				return nil
			}
			lastErr = err
			logger.Printf("remote support callback warning: op=%s attempt=%d err=%v", op, attempt, err)
			if !api.IsRetryableError(err) {
				logger.Printf("remote support callback permanent failure: op=%s attempt=%d", op, attempt)
				break
			}
			if attempt == maxAttempts {
				break
			}
			time.Sleep(time.Duration(attempt*300) * time.Millisecond)
		}
		return lastErr
	}
	sendRemoteApprove := func(sessionID int, approved bool, monitorCount int) (*api.RemoteApproveResponse, error) {
		if sessionID <= 0 {
			return nil, nil
		}
		if strings.TrimSpace(st.SecretKey) == "" {
			return nil, fmt.Errorf("agent not registered yet")
		}
		var out *api.RemoteApproveResponse
		err := callRemoteWithRetry("approve", func(callCtx context.Context) error {
			resp, err := client.RemoteApprove(callCtx, st.UUID, st.SecretKey, sessionID, approved, monitorCount)
			if err != nil {
				return err
			}
			out = resp
			return nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	sendRemoteReady := func(sessionID int, localVNCPort int) error {
		if sessionID <= 0 {
			return nil
		}
		if strings.TrimSpace(st.SecretKey) == "" {
			return fmt.Errorf("agent not registered yet")
		}
		return callRemoteWithRetry("ready", func(callCtx context.Context) error {
			return client.RemoteReady(callCtx, st.UUID, st.SecretKey, sessionID, localVNCPort)
		})
	}
	sendRemoteEnded := func(sessionID int, reason string) error {
		if sessionID <= 0 {
			return nil
		}
		if strings.TrimSpace(st.SecretKey) == "" {
			return fmt.Errorf("agent not registered yet")
		}
		return callRemoteWithRetry("ended", func(callCtx context.Context) error {
			return client.RemoteEnded(callCtx, st.UUID, st.SecretKey, sessionID, "agent", reason)
		})
	}
	approveRemoteSession := func(monitorCount int) error {
		if !cfg.RemoteSupport.Enabled {
			return fmt.Errorf("remote support is disabled by config")
		}
		sst, err := remoteSupportSession.Approve()
		if err != nil {
			return err
		}
		if monitorCount <= 0 {
			monitorCount = 1
		}
		approveResp, err := sendRemoteApprove(sst.SessionID, true, monitorCount)
		if err != nil {
			remoteSupportSession.Error(err)
			persistRemoteSupportSession()
			return err
		}
		if approveResp != nil {
			logger.Printf(
				"remote support approved by server: session_id=%d guacd_host=%s guacd_reverse_port=%d vnc_password_set=%t",
				sst.SessionID,
				strings.TrimSpace(approveResp.GuacdHost),
				approveResp.GuacdReversePort,
				strings.TrimSpace(approveResp.VNCPassword) != "",
			)
		}
		vncPassword := ""
		if approveResp != nil {
			vncPassword = strings.TrimSpace(approveResp.VNCPassword)
		}
		_, err = remoteSupportManager.Start(vncPassword)
		if err != nil {
			remoteSupportSession.Error(err)
			persistRemoteSupportSession()
			return err
		}
		localVNCPort := remoteSupportManager.Snapshot().Port
		if localVNCPort <= 0 {
			localVNCPort = cfg.RemoteSupport.Port
		}
		if err = sendRemoteReady(sst.SessionID, localVNCPort); err != nil {
			_, _ = remoteSupportManager.Stop()
			_ = sendRemoteEnded(sst.SessionID, "ready callback failed")
			remoteSupportSession.Error(err)
			persistRemoteSupportSession()
			return err
		}
		guacdHost := ""
		guacdReversePort := 0
		serverVNCPasswordSet := false
		if approveResp != nil {
			guacdHost = strings.TrimSpace(approveResp.GuacdHost)
			guacdReversePort = approveResp.GuacdReversePort
			serverVNCPasswordSet = strings.TrimSpace(approveResp.VNCPassword) != ""
		}
		remoteSupportSession.SetConnectionInfo(guacdHost, guacdReversePort, localVNCPort, serverVNCPasswordSet)
		remoteSupportSession.Activate()
		persistRemoteSupportSession()
		return nil
	}
	rejectRemoteSession := func(reason string) error {
		if strings.TrimSpace(reason) == "" {
			reason = "rejected by user"
		}
		sst, err := remoteSupportSession.Reject(reason)
		if err != nil {
			return err
		}
		if _, err = sendRemoteApprove(sst.SessionID, false, 0); err != nil {
			return err
		}
		persistRemoteSupportSession()
		return nil
	}
	var approvalPromptMu sync.Mutex
	approvalPromptRunning := make(map[int]bool)
	startApprovalPrompt := func(sessionID int, adminName, reason string) {
		if sessionID <= 0 {
			return
		}
		approvalPromptMu.Lock()
		if approvalPromptRunning[sessionID] {
			approvalPromptMu.Unlock()
			return
		}
		approvalPromptRunning[sessionID] = true
		approvalPromptMu.Unlock()

		go func() {
			defer func() {
				approvalPromptMu.Lock()
				delete(approvalPromptRunning, sessionID)
				approvalPromptMu.Unlock()
			}()
			approved, decided, promptErr := remotesupport.PromptApproval(remoteDisplay, adminName, reason, cfg.RemoteSupport.ApprovalTimeoutSec, logger)
			if promptErr != nil {
				logger.Printf("remote support approval prompt failed: session_id=%d err=%v", sessionID, promptErr)
				return
			}
			cur := remoteSupportSession.Snapshot()
			if cur.SessionID != sessionID || cur.State != remotesupport.StatePending {
				logger.Printf("remote support approval prompt result ignored: session_id=%d state=%s", sessionID, cur.State)
				return
			}
			if !decided {
				logger.Printf("remote support approval prompt closed without decision: session_id=%d", sessionID)
				return
			}
			if approved {
				if err := approveRemoteSession(1); err != nil {
					logger.Printf("remote support prompt approve failed: session_id=%d err=%v", sessionID, err)
					return
				}
				logger.Printf("remote support approved from desktop prompt: session_id=%d", sessionID)
				return
			}
			if err := rejectRemoteSession("rejected by user"); err != nil {
				logger.Printf("remote support prompt reject failed: session_id=%d err=%v", sessionID, err)
				return
			}
			logger.Printf("remote support rejected from desktop prompt: session_id=%d", sessionID)
		}()
	}
	info := system.CollectHostInfo()
	triggerCh := make(chan struct{}, 1)
	var hasSecret atomic.Bool
	hasSecret.Store(st.SecretKey != "")
	if restored := remoteSupportSession.Snapshot(); restored.State == remotesupport.StatePending && restored.RequestedAtUnix > 0 {
		deadline := restored.RequestedAtUnix + int64(cfg.RemoteSupport.ApprovalTimeoutSec)
		if time.Now().Unix() > deadline {
			remoteSupportSession.End("approval timeout after restart")
			persistRemoteSupportSession()
			logger.Printf("remote support session expired on startup: session_id=%d", restored.SessionID)
		}
	}

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
		handler := func(req ipc.Request) ipc.Response {
			okResp := func(message, code string, data any) ipc.Response {
				if strings.TrimSpace(code) == "" {
					code = "ok"
				}
				return ipc.Response{Status: "ok", Code: code, Message: message, Data: data}
			}
			errResp := func(message, code string, err error, data any) ipc.Response {
				if strings.TrimSpace(code) == "" {
					code = "internal_error"
				}
				out := ipc.Response{Status: "error", Code: code, Message: message, Data: data}
				if err != nil {
					out.Error = err.Error()
				} else if strings.TrimSpace(message) != "" {
					out.Error = message
				}
				return out
			}
			remoteSupportSnapshot := func() any {
				return map[string]any{
					"session":           remoteSupportSession.Snapshot(),
					"daemon":            remoteSupportManager.Snapshot(),
					"enabled_by_config": cfg.RemoteSupport.Enabled,
					"in_progress":       remoteSupportSession.InProgress(),
				}
			}
			switch strings.ToLower(strings.TrimSpace(req.Action)) {
			case "ping":
				return okResp("pong", "ok", nil)
			case "store_list":
				if st.SecretKey == "" {
					return errResp("agent not registered yet", "agent_not_registered", nil, nil)
				}
				callCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				resp, err := client.GetStore(callCtx, st.UUID, st.SecretKey)
				if err != nil {
					return errResp("store list failed", "store_list_failed", err, nil)
				}
				return okResp(fmt.Sprintf("store apps fetched: %d", len(resp.Apps)), "store_list_ok", resp.Apps)
			case "store_install":
				if req.AppID <= 0 {
					return errResp("app_id must be positive", "invalid_app_id", nil, nil)
				}
				if st.SecretKey == "" {
					return errResp("agent not registered yet", "agent_not_registered", nil, nil)
				}
				callCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				resp, err := client.RequestStoreInstall(callCtx, st.UUID, st.SecretKey, req.AppID)
				if err != nil {
					return errResp("store install request failed", "store_install_failed", err, nil)
				}
				return okResp(resp.Message, "store_install_requested", nil)
			case "remote_support_env":
				env := remotesupport.ProbeEnv()
				status := "ok"
				msg := "x11vnc is not installed"
				if env.Installed {
					msg = "x11vnc is installed"
				}
				return okResp(msg, status, env)
			case "remote_support_status":
				daemon := remoteSupportManager.Snapshot()
				session := remoteSupportSession.Snapshot()
				if session.State == remotesupport.StateActive && !daemon.Running {
					if strings.TrimSpace(daemon.LastError) != "" {
						remoteSupportSession.Error(fmt.Errorf(daemon.LastError))
					} else {
						remoteSupportSession.End("daemon stopped")
					}
					if err := sendRemoteEnded(session.SessionID, "daemon stopped"); err != nil {
						logger.Printf("remote support ended report warning: %v", err)
					}
					persistRemoteSupportSession()
				}
				return okResp("remote support status", "remote_support_status", remoteSupportSnapshot())
			case "remote_support_session_request":
				sst, err := remoteSupportSession.Begin(req.SessionID, strings.TrimSpace(req.AdminName), strings.TrimSpace(req.Reason))
				if err != nil {
					return errResp("remote support session request failed", "remote_support_session_request_failed", err, sst)
				}
				persistRemoteSupportSession()
				return okResp("remote support session pending approval", "remote_support_session_pending", remoteSupportSnapshot())
			case "remote_support_approve":
				monitorCount := req.MonitorCount
				if err := approveRemoteSession(monitorCount); err != nil {
					return errResp("remote support approve failed", "remote_support_approve_failed", err, remoteSupportSnapshot())
				}
				return okResp("remote support approved and started", "remote_support_approved", remoteSupportSnapshot())
			case "remote_support_reject":
				if err := rejectRemoteSession(strings.TrimSpace(req.Reason)); err != nil {
					return errResp("remote support reject failed", "remote_support_reject_failed", err, remoteSupportSnapshot())
				}
				return okResp("remote support rejected", "remote_support_rejected", remoteSupportSnapshot())
			case "remote_support_clear":
				remoteSupportSession.Clear()
				persistRemoteSupportSession()
				return okResp("remote support state cleared", "remote_support_cleared", remoteSupportSnapshot())
			case "remote_support_end":
				sst := remoteSupportSession.Snapshot()
				_, err := remoteSupportManager.Stop()
				if err != nil {
					return errResp("remote support daemon stop failed", "remote_support_daemon_stop_failed", err, remoteSupportSnapshot())
				}
				remoteSupportSession.End("ended")
				if err = sendRemoteEnded(sst.SessionID, "ended from ipc"); err != nil {
					return errResp("remote support ended callback failed", "remote_support_ended_callback_failed", err, remoteSupportSnapshot())
				}
				persistRemoteSupportSession()
				return okResp("remote support ended", "remote_support_ended", remoteSupportSnapshot())
			case "remote_support_start":
				if !cfg.RemoteSupport.Enabled {
					return errResp("remote support is disabled by config", "remote_support_disabled", nil, remoteSupportSnapshot())
				}
				rst, err := remoteSupportManager.Start("")
				if err != nil {
					return errResp("remote support start failed", "remote_support_start_failed", err, rst)
				}
				return okResp("remote support started", "remote_support_started", rst)
			case "remote_support_stop":
				rst, err := remoteSupportManager.Stop()
				if err != nil {
					return errResp("remote support stop failed", "remote_support_stop_failed", err, rst)
				}
				return okResp("remote support stopped", "remote_support_stopped", rst)
			default:
				return errResp("unsupported action", "unsupported_action", nil, nil)
			}
		}
		if err := ipc.Start(ctx, cfg.IPC.SocketPath, logger, handler); err != nil && ctx.Err() == nil {
			logger.Printf("ipc server stopped with error: %v", err)
		}
	}()

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
	type installJob struct {
		command api.Command
	}
	installQueue := make(chan installJob, cfg.Install.QueueCapacity)
	var activeInstalls atomic.Int32
	for workerID := 1; workerID <= cfg.Install.WorkerCount; workerID++ {
		id := workerID
		go func() {
			logger.Printf("install worker started: id=%d", id)
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-installQueue:
					cmd := job.command
					func() {
						activeInstalls.Add(1)
						defer activeInstalls.Add(-1)

						reportTaskStatus(ctx, client, st.UUID, st.SecretKey, cmd.TaskID, api.TaskStatusRequest{
							Status:   "downloading",
							Progress: 5,
							Message:  "Queued for install",
						}, logger)
						logger.Printf(
							"task=%d start install: app_id=%d version=%s priority=%d force_update=%t worker_id=%d",
							cmd.TaskID, cmd.AppID, strings.TrimSpace(cmd.AppVersion), cmd.Priority, cmd.ForceUpdate, id,
						)
						terminalReported := runInstallCommand(ctx, client, cfg, st.UUID, st.SecretKey, cmd, logger)
						taskGuard.Finish(cmd.TaskID, terminalReported)
						persistTaskDeduper(cfg.Paths.StateFile, st, taskGuard, logger)
					}()
				}
			}
		}()
	}
	var nextInventorySync time.Time
	inventoryInterval := 30 * time.Minute
	var nextServiceSnapshot time.Time
	serviceMonitoringEnabled := false
	serviceSyncRequired := false
	lastServicesHash := ""
	nextSelfUpdateCheck := time.Now()
	selfUpdateInterval := 60 * time.Minute

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
		currentStatus := "Idle"
		if activeInstalls.Load() > 0 || len(installQueue) > 0 {
			currentStatus = "Busy"
		}
		curSessionBeforeHB := remoteSupportSession.Snapshot()
		curDaemonBeforeHB := remoteSupportManager.Snapshot()
		if curSessionBeforeHB.State == remotesupport.StateActive && !curDaemonBeforeHB.Running {
			if strings.TrimSpace(curDaemonBeforeHB.LastError) != "" {
				remoteSupportSession.Error(fmt.Errorf(curDaemonBeforeHB.LastError))
			} else {
				remoteSupportSession.Error(fmt.Errorf("remote support daemon stopped"))
			}
			if err := sendRemoteEnded(curSessionBeforeHB.SessionID, "daemon stopped"); err != nil {
				logger.Printf("remote support ended report warning: %v", err)
			}
			persistRemoteSupportSession()
			logger.Printf("remote support daemon stopped while session active: session_id=%d", curSessionBeforeHB.SessionID)
		}
		rsSession := remoteSupportSession.Snapshot()
		rsDaemon := remoteSupportManager.Snapshot()
		profile := system.CollectSystemProfile()
		apiDisks := make([]api.SystemDisk, 0, len(profile.Disks))
		for _, d := range profile.Disks {
			apiDisks = append(apiDisks, api.SystemDisk{
				Index:   d.Index,
				SizeGB:  d.SizeGB,
				Model:   d.Model,
				BusType: d.BusType,
			})
		}
		var virt *api.VirtualizationInfo
		if profile.Virtualization != nil {
			virt = &api.VirtualizationInfo{
				IsVirtual: profile.Virtualization.IsVirtual,
				Vendor:    profile.Virtualization.Vendor,
				Model:     profile.Virtualization.Model,
			}
		}
		sessions := system.GetLoggedInSessions()
		apiSessions := make([]api.LoggedInSession, 0, len(sessions))
		for _, s := range sessions {
			apiSessions = append(apiSessions, api.LoggedInSession{
				Username:    s.Username,
				SessionType: s.SessionType,
				LogonID:     s.LogonID,
			})
		}
		servicesHash := ""
		servicesPayload := []api.ServiceItem(nil)
		if serviceMonitoringEnabled {
			shouldCollectServices := serviceSyncRequired || lastServicesHash == "" || time.Now().After(nextServiceSnapshot)
			if shouldCollectServices {
				collectCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
				collected, serr := svcmon.Collect(collectCtx)
				cancel()
				if serr != nil {
					logger.Printf("service snapshot collect warning: %v", serr)
				} else {
					servicesHash = svcmon.Hash(collected)
					if serviceSyncRequired || servicesHash != lastServicesHash {
						servicesPayload = collected
					}
					lastServicesHash = servicesHash
					nextServiceSnapshot = time.Now().Add(inventoryInterval)
				}
			} else {
				servicesHash = lastServicesHash
			}
		}
		hb, hbErr := client.Heartbeat(ctx, st.UUID, st.SecretKey, api.HeartbeatRequest{
			Hostname:         info.Hostname,
			IPAddress:        info.IPAddress,
			FullIP:           info.IPAddresses,
			UptimeSec:        info.UptimeSec,
			OSUser:           system.CurrentOSUser(),
			OSVersion:        info.OSVersion,
			CPUModel:         info.CPUModel,
			RAMGB:            info.RAMGB,
			Arch:             info.Arch,
			Distro:           info.Distro,
			DistroVersion:    info.DistroVersion,
			AgentVersion:     cfg.Agent.Version,
			DiskFreeGB:       info.DiskFreeGB,
			CurrentStatus:    currentStatus,
			AppsChanged:      false,
			InstalledApps:    []any{},
			InventoryHash:    st.InventoryHash,
			ServicesHash:     servicesHash,
			Services:         servicesPayload,
			LoggedInSessions: apiSessions,
			Platform:         info.Platform,
			SystemProfile: &api.SystemProfile{
				OSFullName:       profile.OSFullName,
				OSVersion:        profile.OSVersion,
				BuildNumber:      profile.BuildNumber,
				Architecture:     profile.Architecture,
				Manufacturer:     profile.Manufacturer,
				Model:            profile.Model,
				CPUModel:         profile.CPUModel,
				CPUCoresPhysical: profile.CPUCoresPhysical,
				CPUCoresLogical:  profile.CPUCoresLogical,
				TotalMemoryGB:    profile.TotalMemoryGB,
				DiskCount:        profile.DiskCount,
				Disks:            apiDisks,
				Virtualization:   virt,
			},
			RemoteSupport: &api.RemoteSupportHeartbeat{
				State:                rsSession.State,
				SessionID:            rsSession.SessionID,
				HelperRunning:        rsDaemon.Running,
				HelperPID:            rsDaemon.PID,
				GuacdHost:            strings.TrimSpace(rsSession.GuacdHost),
				GuacdReversePort:     rsSession.GuacdReversePort,
				LocalVNCPort:         rsSession.LocalVNCPort,
				ServerVNCPasswordSet: rsSession.ServerVNCPasswordSet,
				ConnectionReady:      strings.TrimSpace(rsSession.State) == remotesupport.StateActive && rsDaemon.Running && rsSession.LocalVNCPort > 0,
			},
		})
		if hbErr != nil {
			logger.Printf("%s heartbeat failed: %v", reason, hbErr)
			return
		}
		logger.Printf("%s heartbeat ok: status=%s commands=%d", reason, hb.Status, len(hb.Commands))
		if hb.Config.InventoryScanIntervalMin > 0 {
			inventoryInterval = time.Duration(hb.Config.InventoryScanIntervalMin) * time.Minute
		}
		serviceMonitoringEnabled = hb.Config.ServiceMonitoringEnabled
		serviceSyncRequired = hb.Config.ServicesSyncRequired
		if serviceMonitoringEnabled && nextServiceSnapshot.IsZero() {
			nextServiceSnapshot = time.Time{}
		}
		if hb.Config.RuntimeUpdateIntervalMin > 0 {
			selfUpdateInterval = time.Duration(hb.Config.RuntimeUpdateIntervalMin) * time.Minute
		}
		if hb.Config.InventorySyncRequired {
			nextInventorySync = time.Time{}
		}
		if time.Now().After(nextSelfUpdateCheck) {
			nextSelfUpdateCheck = time.Now().Add(selfUpdateInterval)
			if maybeApplySelfUpdate(ctx, client, cfg, resolved, st.UUID, st.SecretKey, hb.Config, logger) {
				return
			}
		}
		remoteSupportAllowed := cfg.RemoteSupport.Enabled && hb.Config.RemoteSupportEnabled
		if !remoteSupportAllowed {
			cur := remoteSupportSession.Snapshot()
			if cur.State == remotesupport.StatePending || cur.State == remotesupport.StateApproved || cur.State == remotesupport.StateActive {
				if _, err := remoteSupportManager.Stop(); err != nil {
					logger.Printf("remote support stop warning: %v", err)
				}
				remoteSupportSession.End("disabled by config")
				if err := sendRemoteEnded(cur.SessionID, "disabled by config"); err != nil {
					logger.Printf("remote support ended report warning: %v", err)
				}
				persistRemoteSupportSession()
				logger.Printf("remote support session terminated because feature disabled")
			}
		}
		if remoteSupportAllowed {
			cur := remoteSupportSession.Snapshot()
			if cur.State == remotesupport.StatePending && cur.RequestedAtUnix > 0 {
				deadline := cur.RequestedAtUnix + int64(cfg.RemoteSupport.ApprovalTimeoutSec)
				if time.Now().Unix() > deadline {
					remoteSupportSession.Reject("approval timeout")
					if _, err := sendRemoteApprove(cur.SessionID, false, 0); err != nil {
						logger.Printf("remote support timeout reject report warning: %v", err)
					}
					persistRemoteSupportSession()
					logger.Printf("remote support session timed out waiting approval: session_id=%d", cur.SessionID)
				}
			}
		}
		if hb.RemoteSupportRequest != nil {
			if !remoteSupportAllowed {
				logger.Printf("remote support request ignored: feature disabled")
			} else {
				_, err := remoteSupportSession.Begin(
					hb.RemoteSupportRequest.SessionID,
					strings.TrimSpace(hb.RemoteSupportRequest.AdminName),
					strings.TrimSpace(hb.RemoteSupportRequest.Reason),
				)
				if err != nil {
					logger.Printf("remote support request ignored: %v", err)
				} else {
					persistRemoteSupportSession()
					logger.Printf("remote support request received: session_id=%d", hb.RemoteSupportRequest.SessionID)
					if hb.RemoteSupportRequest.RequiresApproval {
						startApprovalPrompt(
							hb.RemoteSupportRequest.SessionID,
							strings.TrimSpace(hb.RemoteSupportRequest.AdminName),
							strings.TrimSpace(hb.RemoteSupportRequest.Reason),
						)
					} else {
						if err := approveRemoteSession(1); err != nil {
							logger.Printf("remote support auto-approve failed: session_id=%d err=%v", hb.RemoteSupportRequest.SessionID, err)
						} else {
							logger.Printf("remote support auto-approved by policy: session_id=%d", hb.RemoteSupportRequest.SessionID)
						}
					}
				}
			}
		}
		if hb.RemoteSupportEnd != nil && hb.RemoteSupportEnd.SessionID > 0 {
			cur := remoteSupportSession.Snapshot()
			if cur.SessionID == hb.RemoteSupportEnd.SessionID {
				if _, err := remoteSupportManager.Stop(); err != nil {
					logger.Printf("remote support stop warning: %v", err)
				}
				remoteSupportSession.End("ended by server")
				persistRemoteSupportSession()
				if err := sendRemoteEnded(hb.RemoteSupportEnd.SessionID, "ended by server signal"); err != nil {
					logger.Printf("remote support ended report warning: %v", err)
				}
				logger.Printf("remote support end signal handled: session_id=%d", hb.RemoteSupportEnd.SessionID)
			}
		}
		now := time.Now()
		if now.After(nextInventorySync) {
			ok := syncInventory(ctx, client, cfg.Paths.StateFile, st, st.UUID, st.SecretKey, logger)
			if ok {
				nextInventorySync = now.Add(inventoryInterval)
			} else {
				nextInventorySync = now.Add(2 * time.Minute)
			}
		}
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
			select {
			case installQueue <- installJob{command: cmd}:
				logger.Printf("task=%d queued for install", cmd.TaskID)
			default:
				logger.Printf("task=%d install queue is full", cmd.TaskID)
				terminalReported := reportTaskStatus(ctx, client, st.UUID, st.SecretKey, cmd.TaskID, api.TaskStatusRequest{
					Status:   "failed",
					Progress: 100,
					Message:  "Install queue is full",
					Error:    "install queue capacity reached",
				}, logger)
				taskGuard.Finish(cmd.TaskID, terminalReported)
				persistTaskDeduper(cfg.Paths.StateFile, st, taskGuard, logger)
			}
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

func maybeApplySelfUpdate(ctx context.Context, client *api.Client, cfg *config.Config, configPath, agentUUID, secret string, hbCfg api.HeartbeatConfig, logger *log.Logger) bool {
	targetVersion := strings.TrimSpace(hbCfg.LatestAgentVersion)
	currentVersion := strings.TrimSpace(cfg.Agent.Version)
	if targetVersion == "" || currentVersion == "" || targetVersion == currentVersion {
		return false
	}

	downloadURL := strings.TrimSpace(hbCfg.AgentDownloadURL)
	hash := strings.TrimSpace(hbCfg.AgentHash)
	if downloadURL == "" || hash == "" {
		logger.Printf("self-update skipped: current=%s target=%s reason=missing download/hash", currentVersion, targetVersion)
		return false
	}

	logger.Printf("self-update detected: current=%s target=%s", currentVersion, targetVersion)
	fileName := fmt.Sprintf("agent_self_update_%s.bin", sanitizeVersionToken(targetVersion))
	outPath, n, err := client.DownloadToFile(ctx, agentUUID, secret, downloadURL, cfg.Download.TempDir, fileName)
	if err != nil {
		logger.Printf("self-update download failed: %v", err)
		return false
	}
	if n <= 0 {
		logger.Printf("self-update download failed: empty payload")
		_ = os.Remove(outPath)
		return false
	}
	if err := utils.VerifySHA256(outPath, hash); err != nil {
		logger.Printf("self-update hash verification failed: %v", err)
		_ = os.Remove(outPath)
		return false
	}

	exePath, err := os.Executable()
	if err != nil {
		logger.Printf("self-update failed: executable path: %v", err)
		_ = os.Remove(outPath)
		return false
	}
	if realPath, realErr := filepath.EvalSymlinks(exePath); realErr == nil && strings.TrimSpace(realPath) != "" {
		exePath = realPath
	}
	if err := os.Chmod(outPath, 0o755); err != nil {
		logger.Printf("self-update failed: chmod new binary: %v", err)
		_ = os.Remove(outPath)
		return false
	}

	backupPath := exePath + ".bak"
	_ = os.Remove(backupPath)
	if err := os.Rename(exePath, backupPath); err != nil {
		logger.Printf("self-update failed: backup current binary: %v", err)
		_ = os.Remove(outPath)
		return false
	}
	if err := os.Rename(outPath, exePath); err != nil {
		logger.Printf("self-update failed: activate new binary: %v", err)
		_ = os.Rename(backupPath, exePath)
		_ = os.Remove(outPath)
		return false
	}
	_ = os.Remove(backupPath)

	if err := config.UpdateAgentVersion(configPath, targetVersion); err != nil {
		logger.Printf("self-update warning: config version update failed: %v", err)
	}
	cfg.Agent.Version = targetVersion
	logger.Printf("self-update applied: current=%s target=%s bytes=%d", currentVersion, targetVersion, n)

	go func() {
		time.Sleep(2 * time.Second)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	return true
}

func sanitizeVersionToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
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
	logger.Printf("task=%d app=%d install start: action=%s version=%s", cmd.TaskID, cmd.AppID, cmd.Action, cmd.AppVersion)
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
		logger.Printf("task=%d app=%d download completed: bytes=%d file=%s", cmd.TaskID, cmd.AppID, n, filepath.Base(outPath))
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
	installerType := detectLinuxInstallerType(outPath)
	logger.Printf("task=%d app=%d installer run: type=%s args=%q", cmd.TaskID, cmd.AppID, installerType, cmd.InstallArgs)
	stdout, exitCode, installErr := installer.Install(installCtx, outPath, cmd.InstallArgs)
	installSec := int(time.Since(installStart).Seconds())

	if installErr != nil {
		if errors.Is(installErr, context.DeadlineExceeded) || installCtx.Err() == context.DeadlineExceeded {
			msg := "Install timed out"
			if strings.TrimSpace(stdout) != "" {
				msg = msg + " | " + trimOutput(stdout, 1200)
			}
			logger.Printf("task=%d app=%d install failed: type=%s exit=%d download_sec=%d install_sec=%d err=%s", cmd.TaskID, cmd.AppID, installerType, exitCode, downloadSec, installSec, msg)
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
		logger.Printf("task=%d app=%d install failed: type=%s exit=%d download_sec=%d install_sec=%d err=%s", cmd.TaskID, cmd.AppID, installerType, exitCode, downloadSec, installSec, msg)
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
	logger.Printf(
		"task=%d app=%d install success: type=%s exit=%d download_sec=%d install_sec=%d",
		cmd.TaskID,
		cmd.AppID,
		installerType,
		successCode,
		downloadSec,
		installSec,
	)
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

func detectLinuxInstallerType(path string) string {
	lower := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return ".tar.gz"
	case strings.HasSuffix(lower, ".deb"):
		return ".deb"
	case strings.HasSuffix(lower, ".sh"):
		return ".sh"
	default:
		return filepath.Ext(lower)
	}
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

func syncInventory(
	ctx context.Context,
	client *api.Client,
	statePath string,
	st *state.AgentState,
	agentUUID, secret string,
	logger *log.Logger,
) bool {
	invCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	items, err := inventory.Collect(invCtx)
	if err != nil {
		logger.Printf("inventory collect warning: %v", err)
		return false
	}
	hash := inventory.Hash(items)
	if strings.EqualFold(strings.TrimSpace(st.InventoryHash), strings.TrimSpace(hash)) {
		logger.Printf("inventory unchanged: count=%d", len(items))
		return true
	}

	err = client.SubmitInventory(invCtx, agentUUID, secret, api.InventoryRequest{
		InventoryHash: hash,
		SoftwareCount: len(items),
		Items:         items,
	})
	if err != nil {
		logger.Printf("inventory submit warning: %v", err)
		return false
	}
	st.InventoryHash = hash
	if err := state.Save(statePath, st); err != nil {
		logger.Printf("inventory state save warning: %v", err)
	}
	logger.Printf("inventory submitted: count=%d", len(items))
	return true
}

func remoteSupportSessionToState(s remotesupport.SessionStatus) state.RemoteSupportSession {
	return state.RemoteSupportSession{
		State:                s.State,
		SessionID:            s.SessionID,
		AdminName:            s.AdminName,
		Reason:               s.Reason,
		RequestedAtUnix:      s.RequestedAtUnix,
		DecisionAtUnix:       s.DecisionAtUnix,
		Message:              s.Message,
		LastError:            s.LastError,
		GuacdHost:            s.GuacdHost,
		GuacdReversePort:     s.GuacdReversePort,
		LocalVNCPort:         s.LocalVNCPort,
		ServerVNCPasswordSet: s.ServerVNCPasswordSet,
	}
}

func remoteSupportSessionFromState(s state.RemoteSupportSession) remotesupport.SessionStatus {
	return remotesupport.SessionStatus{
		State:                s.State,
		SessionID:            s.SessionID,
		AdminName:            s.AdminName,
		Reason:               s.Reason,
		RequestedAtUnix:      s.RequestedAtUnix,
		DecisionAtUnix:       s.DecisionAtUnix,
		Message:              s.Message,
		LastError:            s.LastError,
		GuacdHost:            s.GuacdHost,
		GuacdReversePort:     s.GuacdReversePort,
		LocalVNCPort:         s.LocalVNCPort,
		ServerVNCPasswordSet: s.ServerVNCPasswordSet,
	}
}
