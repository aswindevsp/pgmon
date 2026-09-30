package backups

import (
	"strings"
	"testing"
	"time"

	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

func TestBackupDashboardShowsRunningRecovery(t *testing.T) {
	ctx := shared.Context{Started: true, KubeEvery: 15 * time.Second}
	ctx.Data.Cluster.Status.Phase = "Setting up primary"
	ctx.Data.Cluster.Status.TargetPrimary = "cnpg-cluster-1"
	ctx.Data.Cluster.Spec.Bootstrap.Recovery.Source = "pluginRecoveryCluster"
	ctx.Data.Cluster.Status.InitializingPVC = []string{"cnpg-cluster-1"}
	var job monitor.JobResource
	job.Metadata.Name = "cnpg-cluster-1-full-recovery"
	job.Metadata.Labels = map[string]string{"cnpg.io/jobRole": "full-recovery", "cnpg.io/instanceName": "cnpg-cluster-1"}
	job.Status.Active = 1
	now := time.Now()
	job.Status.StartTime = &now
	ctx.Data.Jobs = []monitor.JobResource{job}
	var pod monitor.PodResource
	pod.Metadata.Name = "cnpg-cluster-1-full-recovery-kllbd"
	pod.Metadata.Labels = map[string]string{"cnpg.io/jobRole": "full-recovery", "cnpg.io/instanceName": "cnpg-cluster-1"}
	ctx.Data.Pods = []monitor.PodResource{pod}
	ctx.Data.RestoreProgress = monitor.RestoreProgress{Job: job.Metadata.Name, Pod: pod.Metadata.Name, At: time.Now(), Phase: "base", WrittenBytes: 367 * 1024 * 1024, TotalBytes: 870 * 1024 * 1024, HasRate: true, WriteRate: 20 * 1024 * 1024, BackupID: "20260928T124216"}
	text := Render(ctx, 118)
	if !strings.Contains(text, "Cluster recovery") || !strings.Contains(text, "Setting up primary") || !strings.Contains(text, "cnpg-cluster-1-full-recovery") {
		t.Fatal("recovery job missing from backups tab:", text)
	}
	start := strings.Index(text, "Cluster recovery")
	end := strings.Index(text[start:], "╯")
	if start < 0 || end < 0 {
		t.Fatal("cluster recovery panel missing:", text)
	}
	panel := text[start : start+end]
	if !strings.Contains(panel, "Base backup restore") || !strings.Contains(panel, "WAL recovery") || !strings.Contains(panel, "~42.2%") {
		t.Fatal("restore phases should be inside cluster recovery:", panel)
	}
	if !strings.Contains(text, "Est. remaining") || strings.Contains(text, "100.0%") {
		t.Fatal("remaining estimate missing or claimed complete:", text)
	}
	if !strings.Contains(text, "Waiting for base backup extract") || !strings.Contains(text, "WAL remaining unknown") {
		t.Fatal("WAL phase should wait during base extract:", text)
	}
	if strings.Contains(text, "primary pod is unknown") {
		t.Fatal("recovery presented as a SQL collection failure:", text)
	}
	narrow := Render(ctx, 72)
	nStart := strings.Index(narrow, "Cluster recovery")
	nEnd := strings.Index(narrow[nStart:], "╯")
	if nStart < 0 || nEnd < 0 {
		t.Fatal("narrow cluster recovery panel missing:", narrow)
	}
	nPanel := narrow[nStart : nStart+nEnd]
	if !strings.Contains(nPanel, "Base backup restore") || !strings.Contains(nPanel, "WAL recovery") {
		t.Fatal("narrow restore phases should stay inside cluster recovery:", nPanel)
	}
}

func TestRecoveryWindowDoesNotClaimCoverage(t *testing.T) {
	ctx := shared.Context{}
	ctx.Data.Cluster.Status.FirstRecoverabilityPoint = "2026-09-20T12:00:00Z"
	text := strings.Join(recoveryWindowLines(ctx), "\n")
	if !strings.Contains(text, "2026-09-20T12:00:00Z") || !strings.Contains(text, "WAL continuity and restore coverage have not been verified") {
		t.Fatal(text)
	}
	if strings.Contains(text, "restorable to any second") {
		t.Fatal("recovery coverage claimed without verification:", text)
	}
}

func TestBackupHistoryUsesReportedFields(t *testing.T) {
	ctx := shared.Context{}
	older := monitor.BackupResource{}
	older.Metadata.Name = "older"
	older.Metadata.CreationTimestamp = time.Date(2026, 9, 20, 2, 0, 0, 0, time.UTC)
	older.Status.Phase = "completed"
	older.Spec.Method = "plugin"
	newer := monitor.BackupResource{}
	newer.Metadata.Name = "newer"
	newer.Metadata.CreationTimestamp = older.Metadata.CreationTimestamp.Add(time.Hour)
	newer.Status.Phase = "failed"
	ctx.Data.Backups = []monitor.BackupResource{older, newer}
	text := strings.Join(backupHistoryLines(ctx, 100), "\n")
	if strings.Index(text, "newer") >= strings.Index(text, "older") {
		t.Fatal("newest backup should appear first:", text)
	}
	if !strings.Contains(text, "plugin") || !strings.Contains(text, "failed") || strings.Contains(text, "GB") {
		t.Fatal("history should only show reported fields:", text)
	}
}

func TestNarrowBackupHistoryKeepsStatusAndAllRows(t *testing.T) {
	ctx := shared.Context{}
	for i := 0; i < 12; i++ {
		b := monitor.BackupResource{}
		b.Metadata.Name = "backup-" + string(rune('a'+i))
		b.Metadata.CreationTimestamp = time.Date(2026, 9, 20, i, 0, 0, 0, time.UTC)
		b.Status.Phase = "completed"
		ctx.Data.Backups = append(ctx.Data.Backups, b)
	}
	s := monitor.ScheduledBackupResource{}
	s.Metadata.Name = "daily"
	s.Spec.Schedule = "0 2 * * *"
	ctx.Data.Schedules = []monitor.ScheduledBackupResource{s}
	text := strings.Join(backupHistoryLines(ctx, 48), "\n")
	if strings.Count(text, "completed") != 12 || !strings.Contains(text, "backup-a") || !strings.Contains(text, "backup-l") {
		t.Fatal("history should retain status and every backup:", text)
	}
	if !strings.Contains(text, "daily") || !strings.Contains(text, "0 2 * * *") {
		t.Fatal("schedule should be shown:", text)
	}
}
