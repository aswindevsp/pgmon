package monitor

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBackupProcessNamePrefersStatusBackupName(t *testing.T) {
	var b BackupResource
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"manual"},"status":{"phase":"started","backupName":"backup-20261006035748","reconciliationStartedAt":"2026-10-06T03:57:47Z"}}`), &b); err != nil {
		t.Fatal(err)
	}
	if got := BackupProcessName(b); got != "backup-20261006035748" {
		t.Fatalf("expected status backupName, got %q", got)
	}
}

func TestBackupProcessNameDoesNotGuessFromReconciliation(t *testing.T) {
	// Regression: reconciliationStartedAt 03:57:47Z produced backup-20261006035747
	// while the real barman process was backup-20261006035748. Guessing causes
	// "No unique matching backup process". Empty means discover unique process.
	var b BackupResource
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"manual"},"status":{"phase":"started","reconciliationStartedAt":"2026-10-06T03:57:47Z"}}`), &b); err != nil {
		t.Fatal(err)
	}
	if b.Status.BackupName != "" {
		t.Fatal("fixture should have empty backupName")
	}
	if got := BackupProcessName(b); got != "" {
		t.Fatalf("expected empty (discover) when backupName unset, got %q", got)
	}
	_ = time.Now()
}
