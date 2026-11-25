package replication

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"pgmon/internal/monitor"
	"pgmon/internal/tui/views/shared"
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

func TestReplicationDashboardShowsArchiveReplay(t *testing.T) {
	ctx := replicationFixture()
	ctx.Data.ReplayProgress = map[string]monitor.WALReplayProgress{
		"orders-db-3": {
			Pod: "orders-db-3", State: "in archive recovery", Timeline: 4,
			Position: 512 << 10, Start: 256 << 10, Target: 1024 << 10,
			Restarts: 1, RateBytesPerSec: 55.3 * 1024 * 1024, HasRate: true,
		},
	}
	view := ansi.Strip(Render(ctx, 140))
	for _, want := range []string{"Replica WAL catch-up", "orders-db-3", "at 0/00080000", "~512.0 KiB to consistency", "55.3 MiB/s", "ETA ≈<1s", "1 restarts", "control file", "~33.3%"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard missing %q:\n%s", want, view)
		}
	}
	// While replaying, the meter keeps its empty end and stays under 100%.
	meter := false
	for _, line := range replayProgressLines(ctx, 140) {
		if strings.Contains(line, "█") && strings.Contains(line, "░") && strings.Contains(line, "~33.3%") {
			meter = true
		}
	}
	if !meter {
		t.Fatal("missing in-progress replay meter with percentage")
	}
	footnote := strings.Join(replayProgressLines(ctx, 140), "\n")
	for _, want := range []string{"control file every display tick", "minimum recovery point", "first consistent state", "backup end", "20 minutes"} {
		if !strings.Contains(footnote, want) {
			t.Errorf("footnote missing %q:\n%s", want, footnote)
		}
	}
	bar := false
	for _, line := range replayProgressLines(ctx, 140) {
		if strings.Contains(line, "█") {
			bar = true
		}
	}
	if !bar {
		t.Fatal("missing replay progress meter")
	}
	// The fixed manifest target is usable even when the primary's SQL is stale.
	ctx.SQLLive = false
	view = ansi.Strip(Render(ctx, 140))
	for _, want := range []string{"Replica WAL catch-up", "orders-db-3", "~512.0 KiB to consistency"} {
		if !strings.Contains(view, want) {
			t.Errorf("stale dashboard missing %q:\n%s", want, view)
		}
	}
	// A timeline mismatch makes the fixed target unusable.
	ctx.SQLLive = true
	ctx.Data.ReplayProgress["orders-db-3"] = monitor.WALReplayProgress{
		Pod: "orders-db-3", Timeline: 5, Position: 512 << 10, Target: 1024 << 10, HasRate: true,
	}
	view = ansi.Strip(Render(ctx, 140))
	if !strings.Contains(view, "replaying WAL from archive") || strings.Contains(view, "512.0 KiB to consistency") {
		t.Fatalf("timeline mismatch should hide the consistency estimate:\n%s", view)
	}
	// Once the primary is ahead of the backup end, the bar tracks catch-up to it.
	ctx.Data.Sql.CurrentWALLSN = "0/00200000"
	ctx.Data.ReplayProgress["orders-db-3"] = monitor.WALReplayProgress{
		Pod: "orders-db-3", State: "in archive recovery", Timeline: 4,
		Position: 512 << 10, Start: 256 << 10, Target: 1024 << 10,
		Restarts: 1, RateBytesPerSec: 55.3 * 1024 * 1024, HasRate: true,
	}
	view = ansi.Strip(Render(ctx, 140))
	if !strings.Contains(view, "behind primary") {
		t.Fatalf("primary catch-up should name the primary:\n%s", view)
	}
	if strings.Contains(view, "100%") {
		t.Fatalf("replica still behind primary should not show 100%%:\n%s", view)
	}
	// Reaching the backup end fills the meter and labels it explicitly.
	ctx.Data.Sql.CurrentWALLSN = "0/100000"
	ctx.Data.ReplayProgress["orders-db-3"] = monitor.WALReplayProgress{
		Pod: "orders-db-3", State: "in archive recovery", Timeline: 4,
		Position: 1024 << 10, Live: 1024 << 10, Start: 256 << 10, Target: 1024 << 10,
		Restarts: 1, RateBytesPerSec: 55.3 * 1024 * 1024, HasRate: true,
	}
	view = ansi.Strip(Render(ctx, 140))
	if !strings.Contains(view, "100% · backup end reached") {
		t.Fatalf("reached target should be labeled:\n%s", view)
	}
	if !strings.Contains(view, "~0.0 B to consistency") {
		t.Fatalf("reached target should report no remaining bytes:\n%s", view)
	}
	// A live-cursor sample older than five minutes flags a stalled archive fetch.
	ctx.Data.ReplayProgress["orders-db-3"] = monitor.WALReplayProgress{
		Pod: "orders-db-3", State: "in archive recovery", Timeline: 4,
		Position: 512 << 10, Live: 512 << 10, LiveAt: time.Now().Add(-6 * time.Minute),
		Start: 256 << 10, Target: 1024 << 10, Restarts: 1,
		RateBytesPerSec: 55.3 * 1024 * 1024, HasRate: true,
	}
	view = ansi.Strip(Render(ctx, 140))
	if !strings.Contains(view, "no WAL restored for") {
		t.Fatalf("idle live cursor should be visible in the panel:\n%s", view)
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
