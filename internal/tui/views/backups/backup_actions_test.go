package backups

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pgmon/internal/monitor"
	"pgmon/internal/tui/views/shared"
)

func actionCtx() shared.Context {
	ctx := shared.Context{Started: true, KubeEvery: 15 * time.Second, KubeContext: "dev", Namespace: "cnpg", Cluster: "pg"}
	_ = ctx
	return ctx
}

func TestRenderShowsActionHeaderAndSelection(t *testing.T) {
	ctx := actionCtx()
	ctx.Data.KubeAt = time.Now()
	b1 := monitor.BackupResource{}
	b1.Metadata.Name = "run-1"
	b1.Metadata.UID = "uid-1"
	b1.Status.Phase = "running"
	b1.Spec.Cluster.Name = "pg"
	b1.Spec.Method = "barmanObjectStore"
	b1.Status.StartedAt = time.Now().Add(-time.Minute)
	b1.Status.InstanceID.PodName = "postgres-2"
	b2 := monitor.BackupResource{}
	b2.Metadata.Name = "old-1"
	b2.Metadata.UID = "uid-2"
	b2.Status.Phase = "completed"
	b2.Spec.Cluster.Name = "pg"
	b2.Spec.Method = "barmanObjectStore"
	b2.Metadata.CreationTimestamp = time.Now().Add(-time.Hour)
	b2.Status.StartedAt = time.Now().Add(-time.Hour)
	b2.Status.StoppedAt = time.Now().Add(-50 * time.Minute)
	ctx.Data.Backups = []monitor.BackupResource{b1, b2}
	ctx.BackupSelected = "run-1"
	text := Render(ctx, 100)
	for _, want := range []string{"Backup actions", "run-1", "on postgres-2", "n new", "x stop unavailable", "d delete resource"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "›") && !strings.Contains(text, ">") {
		t.Fatalf("selection marker missing:\n%s", text)
	}
	narrow := Render(ctx, 48)
	if !strings.Contains(narrow, "run-1") || !strings.Contains(narrow, "Backup actions") {
		t.Fatalf("narrow render must keep selection and header:\n%s", narrow)
	}
	row := strings.Index(text, "› run-1")
	actions := strings.Index(text, "Backup actions")
	running := strings.Index(text, "Running 1, queued 0")
	if row < 0 || actions < row || running < actions {
		t.Fatalf("selection, actions, and running details should appear in that order:\n%s", text)
	}
}

func TestBackupActionsStayCompact(t *testing.T) {
	ctx := actionCtx()
	ctx.Data.KubeAt = time.Now()
	if err := json.Unmarshal([]byte(`{"spec":{"plugins":[{"name":"barman-cloud.cloudnative-pg.io"}]}}`), &ctx.Data.Cluster); err != nil {
		t.Fatal(err)
	}
	b := monitor.BackupResource{}
	b.Metadata.Name = "backup-1"
	b.Status.Phase = "completed"
	ctx.Data.Backups = []monitor.BackupResource{b}
	ctx.BackupSelected = b.Metadata.Name
	lines := backupActionLines(ctx, 170)
	if len(lines) != 3 {
		t.Fatalf("expected selected backup, controls, and new backup method only; got %v", lines)
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{"backup-1", "completed", "n new", "d delete resource", "x stop unavailable", "plugin", "barman-cloud.cloudnative-pg.io", "pg"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	for _, duplicate := range []string{"context dev", "namespace cnpg", "Esc close", "Only CNPG phase completed"} {
		if strings.Contains(text, duplicate) {
			t.Fatalf("unexpected repeated detail %q in %s", duplicate, text)
		}
	}
}

func TestRenderShowsStartConfirm(t *testing.T) {
	ctx := actionCtx()
	ctx.BackupConfirmKind = "start"
	ctx.BackupConfirmMeth = "barmanObjectStore"
	text := Render(ctx, 100)
	if !strings.Contains(text, "Confirm") || !strings.Contains(text, "barmanObjectStore") || !strings.Contains(text, "pg") {
		t.Fatalf("start confirm missing:\n%s", text)
	}
	if !strings.Contains(text, "y") || !strings.Contains(text, "Esc") {
		t.Fatalf("confirm controls missing:\n%s", text)
	}
}

func TestRenderShowsDeleteConfirmResourceOnly(t *testing.T) {
	ctx := actionCtx()
	ctx.BackupConfirmKind = "delete"
	ctx.BackupConfirmName = "old-1"
	ctx.BackupConfirmPhase = "completed"
	text := Render(ctx, 100)
	if !strings.Contains(text, "Delete Backup resource") || !strings.Contains(text, "old-1") {
		t.Fatalf("delete confirm must say resource-only:\n%s", text)
	}
}

func TestRenderShowsNoticeAndStale(t *testing.T) {
	ctx := actionCtx()
	ctx.BackupNotice = "Start failed: forbidden"
	ctx.Data.ResourceErr = "backups: forbidden"
	text := Render(ctx, 100)
	if !strings.Contains(text, "Start failed") || !strings.Contains(text, "stale") {
		t.Fatalf("notice and stale missing:\n%s", text)
	}
}
