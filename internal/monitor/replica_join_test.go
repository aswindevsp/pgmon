package monitor

import (
	"testing"
	"time"
)

func TestReplicaJoinsMatchesActiveJobToLiveBaseBackup(t *testing.T) {
	var s Snapshot
	s.Cluster.Metadata.Name = "orders-db"
	var job JobResource
	job.Metadata.Labels = map[string]string{"cnpg.io/jobRole": "join", "cnpg.io/instanceName": "orders-db-2"}
	job.Status.Active = 1
	s.Jobs = []JobResource{job}
	s.Sql.BaseBackups = []BaseBackupProgress{
		{ApplicationName: "orders-db-2-join", Phase: "streaming database files", BackupTotal: 1000, BackupStreamed: 125},
		{ApplicationName: "other-backup", BackupTotal: 1000, BackupStreamed: 900},
	}
	joins := ReplicaJoins(s, true)
	if len(joins) != 1 || joins[0].Name != "orders-db-2" || joins[0].Progress == nil {
		t.Fatalf("unexpected joins: %+v", joins)
	}
	if got, ok := joins[0].Progress.Fraction(); !ok || got != 0.125 {
		t.Fatalf("fraction = %v, %v", got, ok)
	}
	stale := ReplicaJoins(s, false)
	if len(stale) != 1 || stale[0].Progress != nil {
		t.Fatalf("stale SQL progress shown: %+v", stale)
	}
}

func TestBaseBackupFractionRequiresEstimate(t *testing.T) {
	if _, ok := (BaseBackupProgress{}).Fraction(); ok {
		t.Fatal("zero byte estimate must not produce a percentage")
	}
	if got, ok := (BaseBackupProgress{BackupTotal: 100, BackupStreamed: 150}).Fraction(); !ok || got != 1 {
		t.Fatalf("estimated total overrun must be clamped: %v, %v", got, ok)
	}
}

func TestBaseBackupRateUsesSameProcessAndElapsedTime(t *testing.T) {
	before := []BaseBackupProgress{{PID: 391, ApplicationName: "orders-db-2-join", Phase: "streaming database files", BackupStreamed: 1024}}
	after := []BaseBackupProgress{{PID: 391, ApplicationName: "orders-db-2-join", Phase: "streaming database files", BackupStreamed: 5120}}
	rates := CalculateBaseBackupRates(before, after, 4*time.Second)
	if got := rates["orders-db-2-join"]; got != 1024 {
		t.Fatalf("rate = %.0f B/s, want 1024 B/s", got)
	}
	after[0].PID = 392
	if rates := CalculateBaseBackupRates(before, after, 4*time.Second); len(rates) != 0 {
		t.Fatalf("new process must collect two samples: %+v", rates)
	}
	after[0].PID = 391
	after[0].BackupStreamed = 512
	if rates := CalculateBaseBackupRates(before, after, 4*time.Second); len(rates) != 0 {
		t.Fatalf("decreased byte count must not produce a rate: %+v", rates)
	}
}
