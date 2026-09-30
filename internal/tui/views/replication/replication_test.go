package replication

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

func replicationFixture() shared.Context {
	ctx := shared.Context{SQLLive: true, RefreshEvery: 5 * time.Second, KubeEvery: 15 * time.Second}
	ctx.Data.Cluster.Status.CurrentPrimary = "orders-db-1"
	ctx.Data.SqlPrimary = "orders-db-1"
	ctx.Data.SqlAt = time.Now()
	ctx.Data.Sql.CurrentWALLSN = "0/100000"
	ctx.Data.Sql.TimelineID = 4
	lag := int64(512)
	ctx.Data.Sql.Replication = []monitor.ReplicationStatus{{
		ApplicationName: "orders-db-2", State: "streaming", SyncState: "sync",
		SentLSN: "0/100000", WriteLSN: "0/100000", FlushLSN: "0/FFF00", ReplayLSN: "0/FFE00", LagBytes: &lag,
	}}
	retained := int64(8192)
	ctx.Data.Sql.ReplicationSlots = []monitor.ReplicationSlotStatus{{SlotName: "orders_db_2", SlotType: "physical", Active: true, RetainedBytes: &retained}}
	ctx.Data.ReplicationLagHistory = map[string][]float64{"orders-db-2": {1024, 512, 256}}
	return ctx
}

func TestWALBacklogRejectsUnknownAndOutOfOrderLSN(t *testing.T) {
	if n, ok := walBacklog("0/100000", "0/FFE00"); !ok || n != 512 {
		t.Fatalf("backlog = %d, %v; want 512 bytes", n, ok)
	}
	for _, stage := range []string{"", "not-an-lsn", "0/100001"} {
		if _, ok := walBacklog("0/100000", stage); ok {
			t.Fatalf("stage %q should not yield a backlog", stage)
		}
	}
}

func TestReplicationDashboardShowsMeasuredFlowTrendAndSlots(t *testing.T) {
	ctx := replicationFixture()
	view := ansi.Strip(Render(ctx, 100))
	for _, want := range []string{"orders-db-1", "timeline 4", "orders-db-2", "replay", "512.0 B behind", "Replay backlog", "orders_db_2", "8.0 KiB"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard missing %q:\n%s", want, view)
		}
	}
}

func TestReplicationDashboardDoesNotPresentStaleSamples(t *testing.T) {
	ctx := replicationFixture()
	ctx.SQLLive = false
	ctx.Data.SqlAt = time.Now().Add(-time.Minute)
	view := ansi.Strip(Render(ctx, 80))
	if strings.Contains(view, "512.0 B") || strings.Contains(view, "orders_db_2") {
		t.Fatalf("stale SQL appeared as live:\n%s", view)
	}
	if !strings.Contains(view, "stale") {
		t.Fatalf("missing stale state:\n%s", view)
	}
}

func TestReplicationTrendWaitsForHistory(t *testing.T) {
	ctx := replicationFixture()
	ctx.Data.ReplicationLagHistory = nil
	view := ansi.Strip(strings.Join(replicationTrendLines(ctx), "\n"))
	if !strings.Contains(view, "collecting lag samples") {
		t.Fatalf("missing collecting state: %s", view)
	}
}

func TestReplicationDashboardShowsReplicaBaseCopy(t *testing.T) {
	ctx := replicationFixture()
	ctx.Data.Cluster.Metadata.Name = "orders-db"
	ctx.Data.Sql.BaseBackups = []monitor.BaseBackupProgress{{
		PID: 391, ApplicationName: "orders-db-2-join", Phase: "streaming database files",
		BackupTotal: 1000, BackupStreamed: 125,
	}}
	ctx.Data.BaseBackupRates = map[string]float64{"orders-db-2-join": 1024}
	view := ansi.Strip(Render(ctx, 100))
	for _, want := range []string{"Replica initialization", "orders-db-2", "12.5% copied", "1.0 KiB/s", "125.0 B / ~1000.0 B"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard missing %q:\n%s", want, view)
		}
	}
	ctx.SQLLive = false
	if view := ansi.Strip(Render(ctx, 100)); strings.Contains(view, "12.5% copied") {
		t.Fatalf("stale base copy progress shown:\n%s", view)
	}
}
