package backups

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"pgmon/internal/monitor"
	"pgmon/internal/tui/views/shared"
)

func TestBaseRestoreNeverClaimsCompletion(t *testing.T) {
	ctx := shared.Context{Started: true, KubeEvery: 15 * time.Second}
	ctx.Data.Cluster.Status.Phase = "Setting up primary"
	ctx.Data.RestoreProgress = monitor.RestoreProgress{At: time.Now(), Phase: "base", WrittenBytes: 200, TotalBytes: 100, HasRate: true, WriteRate: 10}
	text := strings.Join(baseRestoreLines(ctx), "\n")
	if strings.Contains(text, "100.0%") || !strings.Contains(text, "~99.9%") || !strings.Contains(text, "backup_manifest") {
		t.Fatal(text)
	}
	if strings.Contains(text, "Base backup extracted") {
		t.Fatal("base phase claimed extract complete:", text)
	}
}

func TestWALRestoreHasNoRemainingEstimate(t *testing.T) {
	ctx := shared.Context{Started: true, KubeEvery: 15 * time.Second}
	ctx.Data.Cluster.Status.Phase = "Setting up primary"
	ctx.Data.RestoreProgress = monitor.RestoreProgress{
		At: time.Now(), Phase: "wal", WrittenBytes: 100, TotalBytes: 100,
		WALFiles: 12, LastWAL: "0000001B000009C3000000A0", StartWAL: "0000001B000009C300000093",
		RedoLSN: "9C3/A0000028", WALFileRate: 0.4, WALHistory: []float64{0.2, 0.4},
	}
	base := strings.Join(baseRestoreLines(ctx), "\n")
	if !strings.Contains(base, "Base backup extracted") {
		t.Fatal("WAL phase should mark base complete:", base)
	}
	wal := strings.Join(walRestoreLines(ctx), "\n")
	if !strings.Contains(wal, "Applying WAL") || !strings.Contains(wal, "0000001B000009C3000000A0") || !strings.Contains(wal, "9C3/A0000028") {
		t.Fatal(wal)
	}
	if strings.Contains(wal, "Est. remaining") || strings.Contains(wal, "%") {
		t.Fatal("WAL remaining was invented:", wal)
	}
	if !strings.Contains(wal, "WAL remaining unknown") {
		t.Fatal(wal)
	}
}

func TestClusterRecoveryPlacesPhasesToTheRight(t *testing.T) {
	ctx := shared.Context{Started: true, KubeEvery: 15 * time.Second}
	ctx.Data.Cluster.Status.Phase = "Setting up primary"
	ctx.Data.Cluster.Status.TargetPrimary = "cnpg-cluster-1"
	var job monitor.JobResource
	job.Metadata.Name = "cnpg-cluster-1-full-recovery"
	job.Metadata.Labels = map[string]string{"cnpg.io/jobRole": "full-recovery"}
	job.Status.Active = 1
	ctx.Data.Jobs = []monitor.JobResource{job}
	ctx.Data.RestoreProgress = monitor.RestoreProgress{At: time.Now(), Phase: "base", WrittenBytes: 367 * 1024 * 1024, TotalBytes: 870 * 1024 * 1024, HasRate: true, WriteRate: 20 * 1024 * 1024}
	lines := clusterRecoveryLines(ctx, 118)
	if len(lines) == 0 {
		t.Fatal("expected recovery lines")
	}
	if !strings.Contains(lines[0], "Cluster recovery in progress") || !strings.Contains(lines[0], "Base backup restore") {
		t.Fatal("base restore should sit to the right of the job:", lines[0])
	}
	for _, line := range lines {
		if strings.Contains(line, "Base backup restore") && strings.Contains(line, "WAL recovery") {
			t.Fatal("phases should stack on the right, not sit side by side:", line)
		}
	}
	foundWAL := false
	for _, line := range lines {
		if strings.Contains(line, "WAL recovery") {
			foundWAL = true
			break
		}
	}
	if !foundWAL {
		t.Fatal("WAL recovery missing from right column:", strings.Join(lines, "\n"))
	}
	wide := clusterRecoveryLines(ctx, 180)
	idx := strings.Index(wide[0], "Base backup restore")
	if idx < 0 || lipgloss.Width(wide[0][:idx]) < shared.DashboardInnerWidth(180)/2-6 {
		t.Fatal("right column should sit in the right half, not hug the job details:", wide[0])
	}
}
