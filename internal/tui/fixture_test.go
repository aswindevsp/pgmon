package tui

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"pgmon/internal/monitor"
)

func fixture(t *testing.T, replicas int) model {
	t.Helper()
	m := model{cfg: monitor.Config{Context: "development", Namespace: "cnpg", Cluster: "postgres"}, width: 120, height: 40, started: true}
	m.data.Cluster.Status.CurrentPrimary = "postgres-1"
	m.data.Cluster.Spec.Instances = replicas + 1
	m.data.Cluster.Status.ReadyInstances = replicas + 1
	m.data.SqlAt, m.data.KubeAt = time.Now(), time.Now()
	m.data.SqlPrimary = "postgres-1"
	if err := json.Unmarshal([]byte(`{"version":"17.5","wal":{"wal_bytes":10000},"activity":{"connections":24,"active":3,"blocked":0}}`), &m.data.Sql); err != nil {
		t.Fatal(err)
	}
	m.data.WalRate = 2048
	for i := 1; i <= replicas+1; i++ {
		var p monitor.PodResource
		raw := fmt.Sprintf(`{"metadata":{"name":"postgres-%d"},"spec":{"nodeName":"worker-%d"},"status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":0}]}}`, i, i)
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		m.data.Pods = append(m.data.Pods, p)
		if i > 1 {
			lag := "00:00:00.002"
			m.data.Sql.Replication = append(m.data.Sql.Replication, monitor.ReplicationStatus{ApplicationName: p.Metadata.Name, State: "streaming", SyncState: "async", ReplayLag: &lag, SentLSN: "0/100", ReplayLSN: "0/100"})
		}
	}
	return m
}

func fixtureForBackup(t *testing.T) model {
	t.Helper()
	m := fixture(t, 1)
	m.focus = 2
	_ = json.Unmarshal([]byte(`{"spec":{"backup":{"barmanObjectStore":{"destinationPath":"s3://b/"}}}}`), &m.data.Cluster)
	mk := func(name, phase string, hour int, uid string) monitor.BackupResource {
		var b monitor.BackupResource
		raw := fmt.Sprintf(`{"metadata":{"name":%q,"uid":%q,"creationTimestamp":"2026-09-20T%02d:00:00Z"},"spec":{"cluster":{"name":"postgres"},"method":"barmanObjectStore"},"status":{"phase":%q}}`, name, uid, hour, phase)
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	m.data.Backups = []monitor.BackupResource{
		mk("older", "completed", 2, "uid-older"),
		mk("newer", "failed", 3, "uid-newer"),
		mk("run-1", "running", 4, "uid-run-1"),
	}
	m.backupSel = "newer"
	return m
}
