package monitor

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSelectActiveBeforeNewerPending(t *testing.T) {
	var active, pending, other BackupResource
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"manual","creationTimestamp":"2026-09-28T12:42:15Z"},"spec":{"cluster":{"name":"cnpg-cluster"}},"status":{"phase":"started","reconciliationStartedAt":"2026-09-28T12:42:15Z","instanceID":{"podName":"replica"}}}`), &active); err != nil {
		t.Fatal(err)
	}
	pending.Metadata.Name = "scheduled"
	pending.Metadata.CreationTimestamp = active.Metadata.CreationTimestamp.Add(time.Hour)
	pending.Status.Phase = "pending"
	other.Status.Phase = "completed"
	got, ok := SelectedBackup([]BackupResource{pending, other, active})
	if !ok || got.Metadata.Name != "manual" {
		t.Fatal("pending backup hid active backup", got)
	}
	if BackupStart(got).IsZero() || got.Status.InstanceID.PodName != "replica" {
		t.Fatal("plugin status fields not decoded")
	}
}
