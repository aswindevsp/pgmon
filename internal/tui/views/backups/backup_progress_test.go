package backups

import (
	"strings"
	"testing"
	"time"

	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

func TestProgressDoesNotClaimCompletion(t *testing.T) {
	var b monitor.BackupResource
	b.Metadata.Name = "backup"
	b.Metadata.CreationTimestamp = time.Now().Add(-time.Hour)
	b.Status.Phase = "started"
	ctx := shared.Context{KubeEvery: 15 * time.Second}
	ctx.Data.BackupProgress = monitor.BackupProgress{Backup: "backup", At: time.Now(), ReadBytes: 200, TotalBytes: 100, HasRate: true, ReadRate: 10}
	text := strings.Join(backupProgressLines(ctx, b), "\n")
	if strings.Contains(text, "100.0%") || !strings.Contains(text, "~99.9%") || !strings.Contains(text, "not upload completion") {
		t.Fatal(text)
	}
	ctx.Data.BackupProgress.At = time.Now().Add(-35 * time.Second)
	ctx.KubeBusy = true
	text = strings.Join(backupProgressLines(ctx, b), "\n")
	if strings.Contains(text, "stale") || !strings.Contains(text, "throughput") {
		t.Fatal(text)
	}
	ctx.KubeBusy = false
	ctx.Data.BackupProgress.At = time.Now().Add(-time.Minute)
	text = strings.Join(backupProgressLines(ctx, b), "\n")
	if !strings.Contains(text, "stale") || strings.Contains(text, "throughput") {
		t.Fatal(text)
	}
	ctx.Data.BackupProgress.At = time.Now()
	ctx.Data.BackupProgress.Error = "metrics unavailable"
	if text = strings.Join(backupProgressLines(ctx, b), "\n"); !strings.Contains(text, "metrics unavailable") || strings.Contains(text, "~99.9%") {
		t.Fatal(text)
	}
	b.Status.Phase = "completed"
	if len(backupProgressLines(ctx, b)) != 0 {
		t.Fatal("completed backup retained live metrics")
	}
}
