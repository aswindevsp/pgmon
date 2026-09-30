package overview

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
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
