package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"appcenter-agent-linux/internal/api"
	"appcenter-agent-linux/internal/state"
)

func TestDownloadWithRetryRecoversAfterTransientErrors(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pkg.bin" {
			http.NotFound(w, r)
			return
		}
		n := calls.Add(1)
		if n <= 2 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "payload")
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	client := api.NewClient(srv.URL)
	cmd := api.Command{TaskID: 101, AppID: 1, DownloadURL: srv.URL + "/pkg.bin"}
	logger := log.New(io.Discard, "", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outPath, n, err := downloadWithRetry(ctx, client, "uuid", "secret", cmd, tmpDir, "pkg.bin", logger)
	if err != nil {
		t.Fatalf("downloadWithRetry error: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", calls.Load())
	}
	if n != int64(len("payload")) {
		t.Fatalf("unexpected payload size: %d", n)
	}
	b, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("read out file: %v", readErr)
	}
	if string(b) != "payload" {
		t.Fatalf("unexpected payload: %q", string(b))
	}
}

func TestTaskDeduperRetryCooldownAndBurstProtection(t *testing.T) {
	t.Parallel()

	d := newTaskDeduper(2 * time.Second)
	d.retryCooldown = 50 * time.Millisecond
	taskID := 333

	if !d.TryStart(taskID) {
		t.Fatalf("first TryStart must pass")
	}
	if d.TryStart(taskID) {
		t.Fatalf("duplicate inflight TryStart must fail")
	}
	d.Finish(taskID, false)

	if d.TryStart(taskID) {
		t.Fatalf("retry cooldown must block immediate restart")
	}
	time.Sleep(60 * time.Millisecond)
	if !d.TryStart(taskID) {
		t.Fatalf("TryStart must pass after retry cooldown")
	}
	d.Finish(taskID, true)

	if d.TryStart(taskID) {
		t.Fatalf("completed task should be deduped within ttl")
	}
}

func TestTaskDeduperSeedIgnoresStaleEntries(t *testing.T) {
	t.Parallel()

	d := newTaskDeduper(2 * time.Second)
	now := time.Now()
	d.Seed([]state.ProcessedTask{
		{TaskID: 1, ExecutedAtUnix: now.Unix()},
		{TaskID: 2, ExecutedAtUnix: now.Add(-5 * time.Second).Unix()},
	})
	if d.TryStart(1) {
		t.Fatalf("fresh seeded task must be deduped")
	}
	if !d.TryStart(2) {
		t.Fatalf("stale seeded task must be allowed")
	}

}
