package tui

import (
	"fmt"
	"testing"
	"time"

	"pgmon/internal/monitor"
)

func testBackups() []monitor.BackupResource {
	mk := func(name, phase string, hour int) monitor.BackupResource {
		var b monitor.BackupResource
		b.Metadata.Name = name
		b.Metadata.UID = "uid-" + name
		b.Metadata.CreationTimestamp = time.Date(2026, 9, 20, hour, 0, 0, 0, time.UTC)
		b.Status.Phase = phase
		b.Spec.Cluster.Name = "pg"
		return b
	}
	return []monitor.BackupResource{
		mk("older", "completed", 2),
		mk("newer", "failed", 3),
		mk("run-1", "running", 4),
	}
}

func TestNextBackupSelectionCyclesByName(t *testing.T) {
	backups := testBackups()
	// Newest first: run-1, newer, older
	if got := nextBackupSelection(backups, "", 1); got != "run-1" {
		t.Fatalf("empty should select newest, got %q", got)
	}
	if got := nextBackupSelection(backups, "run-1", 1); got != "newer" {
		t.Fatalf("down should move to newer, got %q", got)
	}
	if got := nextBackupSelection(backups, "older", 1); got != "run-1" {
		t.Fatalf("wrap expected, got %q", got)
	}
	if got := nextBackupSelection(backups, "run-1", -1); got != "older" {
		t.Fatalf("up wrap expected, got %q", got)
	}
}

func TestReconcileBackupSelectionPinsIdentity(t *testing.T) {
	backups := testBackups()
	if got := reconcileBackupSelection(backups, "newer"); got != "newer" {
		t.Fatalf("existing must stay, got %q", got)
	}
	if got := reconcileBackupSelection(backups, "gone"); got != "" {
		t.Fatalf("missing must clear, got %q", got)
	}
	// Name reused with different UID is still same name; UID check happens at action time.
	if got := reconcileBackupSelection(backups, ""); got != "run-1" {
		t.Fatalf("empty should default to newest, got %q", got)
	}
}

func TestReconcileBackupSelectionStaysInLastFive(t *testing.T) {
	var backups []monitor.BackupResource
	for i := 0; i < 8; i++ {
		var b monitor.BackupResource
		b.Metadata.Name = fmt.Sprintf("b%d", i)
		b.Metadata.CreationTimestamp = time.Date(2026, 9, 20, i, 0, 0, 0, time.UTC)
		backups = append(backups, b)
	}
	if got := reconcileBackupSelection(backups, "b0"); got != "b7" {
		t.Fatalf("backup outside last five should snap to newest, got %q", got)
	}
	if got := reconcileBackupSelection(backups, "b6"); got != "b6" {
		t.Fatalf("visible backup must stay, got %q", got)
	}
	if got := nextBackupSelection(backups, "b7", 1); got != "b6" {
		t.Fatalf("arrows should stay inside last five, got %q", got)
	}
}

func TestRefreshKeepsConfirmOpen(t *testing.T) {
	m := fixtureForBackup(t)
	m.backupConfirm = &backupConfirm{kind: "delete", name: "newer", uid: "uid-newer"}
	// Simulate kube refresh that still contains the backup.
	updated := m.reconcileAfterKube()
	if updated.backupConfirm == nil || updated.backupConfirm.name != "newer" {
		t.Fatal("confirm must survive refresh while resource exists")
	}
	// Simulate refresh where resource disappears: selection clears but confirm stays for error reporting.
	m.data.Backups = nil
	updated = m.reconcileAfterKube()
	if updated.backupSel != "" {
		t.Fatalf("selection must clear when resource disappears, got %q", updated.backupSel)
	}
	if updated.backupConfirm == nil {
		t.Fatal("confirm should stay open to report missing resource on confirm")
	}
}
