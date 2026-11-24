package overview

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"pgmon/internal/monitor"
	"pgmon/internal/tui/views/shared"
)

func readyPod(t *testing.T, name string, ready bool) monitor.PodResource {
	t.Helper()
	var p monitor.PodResource
	raw := fmt.Sprintf(`{"metadata":{"name":%q},"status":{"phase":"Running","containerStatuses":[{"ready":%t}]}}`, name, ready)
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// sampleCluster builds a healthy three-instance CNPG snapshot with pods, SQL
// metrics, a running backup, and a schedule.
func sampleCluster(t *testing.T) shared.Context {
	t.Helper()
	ctx := shared.Context{Started: true, SQLLive: true, KubeEvery: 15 * time.Second}
	ctx.Data.Cluster.Metadata.Name = "cnpg-cluster"
	ctx.Data.Cluster.Spec.Instances = 3
	ctx.Data.Cluster.Status.CurrentPrimary = "cnpg-cluster-17"
	ctx.Data.Cluster.Status.ReadyInstances = 3
	ctx.Data.WalRate = 0
	ctx.Data.WalHistory = []float64{0, 0, 0, 0, 0, 0, 0}
	ctx.Data.TxHistory = []float64{4, 7, 3, 9, 2, 6, 8, 5}
	ctx.Data.TxRate = 6.8
	sql := `{"version":"17.10","max_connections":100,"current_wal_lsn":"0/16A3B4C0","timeline_id":1,
		"wal":{"wal_bytes":10000,"track_wal_io_timing":"on"},
		"activity":{"connections":19,"active":1,"idle":17,"idle_in_xact":1,"blocked":0,"longest_seconds":4.2},
		"database":{"cache_hit":73.6,"deadlocks":0,"temp_files":7917,"temp_bytes":1288490188},
		"archiver":{"failed_count":0,"last_archived_time":"2026-10-05T10:00:00Z"},
		"replication_slots":[{"slot_name":"cnpg-cluster_rs","active":true},{"slot_name":"stale","active":false}]}`
	if err := json.Unmarshal([]byte(sql), &ctx.Data.Sql); err != nil {
		t.Fatal(err)
	}
	pods := []struct {
		name, node string
		cpu        float64
		mem        uint64
	}{
		{"cnpg-cluster-17", "aks-vmss000000", 0.28, 16 << 30},
		{"cnpg-cluster-13", "aks-vmss000001", 1.02, 9 << 30},
		{"cnpg-cluster-15", "aks-vmss000002", 0.014, 8 << 30},
	}
	for _, p := range pods {
		var pod monitor.PodResource
		raw := fmt.Sprintf(`{"metadata":{"name":%q},"spec":{"nodeName":%q,"containers":[{"name":"postgres","resources":{"limits":{"cpu":"3.86","memory":"30.2Gi"}}}]},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}`, p.name, p.node)
		if err := json.Unmarshal([]byte(raw), &pod); err != nil {
			t.Fatal(err)
		}
		ctx.Data.Pods = append(ctx.Data.Pods, pod)
	}
	ctx.Data.PodUsage = map[string]monitor.ResourceUsage{
		"cnpg-cluster-17": {CPUCores: 0.28, MemoryBytes: 16<<30 + 300<<20},
		"cnpg-cluster-13": {CPUCores: 1.02, MemoryBytes: 9 << 30},
		"cnpg-cluster-15": {CPUCores: 0.014, MemoryBytes: 8<<30 + 700<<20},
	}
	lag := "00:00:00.203"
	zero := int64(0)
	ctx.Data.Sql.Replication = []monitor.ReplicationStatus{
		{ApplicationName: "cnpg-cluster-13", State: "streaming", ReplayLag: &lag, LagBytes: &zero},
		{ApplicationName: "cnpg-cluster-15", State: "streaming", ReplayLag: &lag, LagBytes: &zero},
	}
	b := monitor.BackupResource{}
	b.Metadata.Name = "daily-backup-20260921000000"
	b.Metadata.CreationTimestamp = time.Now().Add(-4 * time.Hour)
	b.Status.Phase = "completed"
	s := monitor.ScheduledBackupResource{}
	s.Spec.Schedule = "0 0 * * *"
	ctx.Data.Backups = []monitor.BackupResource{b}
	ctx.Data.Schedules = []monitor.ScheduledBackupResource{s}
	return ctx
}

func TestOverviewWideShowsVitalsWithoutEventsPanel(t *testing.T) {
	ctx := sampleCluster(t)
	view := ansi.Strip(Render(ctx, 120))
	for _, want := range []string{
		"cnpg-cluster-17", "cnpg-cluster-13", "cnpg-cluster-15",
		"CPU", "MEM", "/ 3.86",
		"19 / 100", "6.8/s", "73.6%",
		"203ms", "4.2s",
		"completed", "daily-backup-20260921000000",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("overview missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "No recent cluster events reported") {
		t.Errorf("empty events should not own a panel:\n%s", view)
	}
	if strings.Contains(view, "5 Backups") || strings.Contains(view, "3 Backups") {
		t.Errorf("backup tab hint leaked into overview:\n%s", view)
	}
}

func TestOverviewNarrowKeepsPrimaryAndBackup(t *testing.T) {
	ctx := sampleCluster(t)
	view := ansi.Strip(Render(ctx, 80))
	for _, want := range []string{"cnpg-cluster-17", "completed", "daily-backup-20260921000000"} {
		if !strings.Contains(view, want) {
			t.Errorf("narrow overview missing %q:\n%s", want, view)
		}
	}
}

func TestOverviewSelectsPrimaryByDefault(t *testing.T) {
	ctx := sampleCluster(t)
	if view := ansi.Strip(Render(ctx, 120)); !strings.Contains(view, "▸ PRIMARY  cnpg-cluster-17") {
		t.Errorf("primary should carry the selection marker by default:\n%s", view)
	}
	ctx.SelectedPod = "cnpg-cluster-15"
	view := ansi.Strip(Render(ctx, 120))
	if !strings.Contains(view, "▸ REPLICA  cnpg-cluster-15") || strings.Contains(view, "▸ PRIMARY") {
		t.Errorf("selection should follow SelectedPod:\n%s", view)
	}
}

func TestOverviewAttentionWithoutBackups(t *testing.T) {
	ctx := sampleCluster(t)
	ctx.Data.Backups, ctx.Data.Schedules = nil, nil
	view := ansi.Strip(Render(ctx, 120))
	if !strings.Contains(view, "no backups or schedules") {
		t.Errorf("missing backup attention:\n%s", view)
	}
	if strings.Contains(view, "╭─ Backup") {
		t.Errorf("empty backup panel should be skipped:\n%s", view)
	}
}

func TestClusterHealthFlagsUnreadyPod(t *testing.T) {
	ctx := shared.Context{Started: true, SQLLive: true}
	ctx.Data.Cluster.Status.CurrentPrimary = "postgres-1"
	ctx.Data.Cluster.Spec.Instances = 2
	ctx.Data.Cluster.Status.ReadyInstances = 2
	ctx.Data.Pods = []monitor.PodResource{readyPod(t, "postgres-1", true), readyPod(t, "postgres-2", false)}
	if strings.Contains(clusterHealth(ctx), "All instances ready") {
		t.Fatal("running but unready pod presented as healthy")
	}
}

func TestClusterHealthShowsRecoveryPhase(t *testing.T) {
	ctx := shared.Context{Started: true, SQLLive: true}
	ctx.Data.Cluster.Status.Phase = "Setting up primary"
	ctx.Data.Cluster.Status.CurrentPrimary = ""
	ctx.Data.Cluster.Status.TargetPrimary = "postgres-1"
	if !strings.Contains(clusterHealth(ctx), "Setting up primary") {
		t.Fatal("recovery phase not shown:", clusterHealth(ctx))
	}
}

func TestOverviewShowsSwitchoverConfirmAndProgress(t *testing.T) {
	ctx := sampleCluster(t)
	ctx.SelectedPod = "cnpg-cluster-15"
	ctx.Cluster = "cnpg-cluster"
	ctx.Namespace = "cnpg"
	ctx.SwitchoverConfirmPrimary = "cnpg-cluster-17"
	ctx.SwitchoverConfirmTarget = "cnpg-cluster-15"
	view := ansi.Strip(Render(ctx, 120))
	if !strings.Contains(view, "Confirm switchover") || !strings.Contains(view, "cnpg-cluster-15") {
		t.Fatalf("overview confirm missing:\n%s", view)
	}
	ctx.SwitchoverConfirmPrimary, ctx.SwitchoverConfirmTarget = "", ""
	ctx.Data.Cluster.Status.TargetPrimary = "cnpg-cluster-15"
	view = ansi.Strip(Render(ctx, 120))
	if !strings.Contains(view, "switchover") || !strings.Contains(view, "cnpg-cluster-15") {
		t.Fatalf("in-progress switchover missing:\n%s", view)
	}
}

func TestClusterHealthShowsSwitchover(t *testing.T) {
	ctx := shared.Context{Started: true, SQLLive: true}
	ctx.Data.Cluster.Status.CurrentPrimary = "postgres-1"
	ctx.Data.Cluster.Status.TargetPrimary = "postgres-2"
	ctx.Data.Cluster.Spec.Instances = 2
	ctx.Data.Cluster.Status.ReadyInstances = 2
	if !strings.Contains(clusterHealth(ctx), "switchover to postgres-2") {
		t.Fatal("switchover phase not shown:", clusterHealth(ctx))
	}
}

func TestOverviewShowsReplicaCopySpeed(t *testing.T) {
	ctx := shared.Context{Started: true, SQLLive: true}
	ctx.Data.Cluster.Metadata.Name = "orders-db"
	ctx.Data.Cluster.Status.CurrentPrimary = "orders-db-1"
	ctx.Data.Sql.BaseBackups = []monitor.BaseBackupProgress{{PID: 391, ApplicationName: "orders-db-2-join", Phase: "streaming database files", BackupTotal: 1000, BackupStreamed: 125}}
	ctx.Data.BaseBackupRates = map[string]float64{"orders-db-2-join": 1024}
	view := ansi.Strip(Render(ctx, 100))
	for _, want := range []string{"orders-db-2", "base copy 12.5%", "1.0 KiB/s"} {
		if !strings.Contains(view, want) {
			t.Errorf("overview missing %q:\n%s", want, view)
		}
	}
}
