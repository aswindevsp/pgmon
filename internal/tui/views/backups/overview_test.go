package backups

import (
	"strings"
	"testing"
	"time"

	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

func TestOverviewLinesCompletedIsOneLine(t *testing.T) {
	ctx := shared.Context{Started: true}
	b := monitor.BackupResource{}
	b.Metadata.Name = "daily-backup-1"
	b.Metadata.CreationTimestamp = time.Now().Add(-4 * time.Hour)
	b.Status.Phase = "completed"
	s := monitor.ScheduledBackupResource{}
	s.Spec.Schedule = "0 0 * * *"
	ctx.Data.Backups = []monitor.BackupResource{b}
	ctx.Data.Schedules = []monitor.ScheduledBackupResource{s}

	lines := OverviewLines(ctx)
	if len(lines) != 1 {
		t.Fatalf("completed backup should be one line, got %d: %v", len(lines), lines)
	}
	for _, want := range []string{"completed", "daily-backup-1", "cron 0 0 * * *", "4h ago"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("missing %q in %q", want, lines[0])
		}
	}
}

func TestOverviewLinesRunningShowsElapsedAndRead(t *testing.T) {
	ctx := shared.Context{Started: true}
	b := monitor.BackupResource{}
	b.Metadata.Name = "daily-backup-2"
	b.Status.Phase = "running"
	b.Status.StartedAt = time.Now().Add(-2 * time.Hour)
	b.Status.InstanceID.PodName = "cnpg-cluster-13"
	ctx.Data.Backups = []monitor.BackupResource{b}
	ctx.Data.BackupProgress = monitor.BackupProgress{Backup: "daily-backup-2", ReadBytes: 2 << 40}

	lines := OverviewLines(ctx)
	if len(lines) != 1 {
		t.Fatalf("running backup should be one line, got %d: %v", len(lines), lines)
	}
	for _, want := range []string{"running", "daily-backup-2", "elapsed", "2.0 TiB read", "on cnpg-cluster-13"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("missing %q in %q", want, lines[0])
		}
	}
	if strings.Contains(lines[0], "Last") || strings.Contains(lines[0], "Started") {
		t.Errorf("running backup repeated timestamps: %q", lines[0])
	}
}

func TestOverviewLinesCompletedShowsPod(t *testing.T) {
	ctx := shared.Context{Started: true}
	b := monitor.BackupResource{}
	b.Metadata.Name = "daily-backup-1"
	b.Metadata.CreationTimestamp = time.Now().Add(-4 * time.Hour)
	b.Status.Phase = "completed"
	b.Status.InstanceID.PodName = "postgres-3"
	ctx.Data.Backups = []monitor.BackupResource{b}
	lines := OverviewLines(ctx)
	if len(lines) != 1 || !strings.Contains(lines[0], "on postgres-3") {
		t.Fatalf("completed backup should name the pod: %v", lines)
	}
}

func TestOverviewLinesFailedShowsError(t *testing.T) {
	ctx := shared.Context{Started: true}
	b := monitor.BackupResource{}
	b.Metadata.Name = "daily-backup-3"
	b.Status.Phase = "failed"
	b.Status.Error = "barman-cloud upload rejected"
	ctx.Data.Backups = []monitor.BackupResource{b}

	lines := OverviewLines(ctx)
	if len(lines) != 1 || !strings.Contains(lines[0], "failed") || !strings.Contains(lines[0], "barman-cloud") {
		t.Fatalf("failed backup line wrong: %v", lines)
	}
}

func TestOverviewLinesNoBackups(t *testing.T) {
	ctx := shared.Context{Started: true}
	if got := OverviewLines(ctx); len(got) != 1 || !strings.Contains(got[0], "No backup") {
		t.Fatalf("expected a muted no-backup line, got %v", got)
	}
	ctx.Data.Schedules = []monitor.ScheduledBackupResource{{}}
	ctx.Data.Schedules[0].Spec.Schedule = "0 1 * * *"
	if got := OverviewLines(ctx); len(got) != 1 || !strings.Contains(got[0], "0 1 * * *") {
		t.Fatalf("schedule should be shown with no backups, got %v", got)
	}
}
