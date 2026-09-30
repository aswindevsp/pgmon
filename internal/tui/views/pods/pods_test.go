package pods

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

func fixture(t *testing.T, replicas int) shared.Context {
	t.Helper()
	ctx := shared.Context{Started: true, SQLLive: true, RefreshEvery: 5 * time.Second, KubeEvery: 15 * time.Second, SelectedPod: "postgres-1"}
	ctx.Data.Cluster.Status.CurrentPrimary = "postgres-1"
	ctx.Data.Cluster.Spec.Instances = replicas + 1
	ctx.Data.Cluster.Status.ReadyInstances = replicas + 1
	ctx.Data.SqlAt, ctx.Data.KubeAt = time.Now(), time.Now()
	ctx.Data.SqlPrimary = "postgres-1"
	if err := json.Unmarshal([]byte(`{"version":"17.5","wal":{"wal_bytes":10000},"activity":{"connections":24,"active":3,"blocked":0}}`), &ctx.Data.Sql); err != nil {
		t.Fatal(err)
	}
	ctx.Data.WalRate = 2048
	for i := 1; i <= replicas+1; i++ {
		var p monitor.PodResource
		raw := fmt.Sprintf(`{"metadata":{"name":"postgres-%d"},"spec":{"nodeName":"worker-%d"},"status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":0}]}}`, i, i)
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		ctx.Data.Pods = append(ctx.Data.Pods, p)
		if i > 1 {
			lag := "00:00:00.002"
			ctx.Data.Sql.Replication = append(ctx.Data.Sql.Replication, monitor.ReplicationStatus{ApplicationName: p.Metadata.Name, State: "streaming", SyncState: "async", ReplayLag: &lag, SentLSN: "0/100", ReplayLSN: "0/100"})
		}
	}
	return ctx
}

func TestTopologyBranches(t *testing.T) {
	for _, replicas := range []int{0, 1, 2, 5} {
		for _, width := range []int{32, 48, 80, 98, 120, 160} {
			ctx := fixture(t, replicas)
			graph := Topology(ctx, width-2)
			// Each replica must receive exactly one incoming branch at every size.
			if got := strings.Count(graph, "→") - 1; got != replicas {
				t.Errorf("replicas=%d width=%d: got %d branches", replicas, width, got)
			}
			for _, line := range strings.Split(graph, "\n") {
				if lipgloss.Width(line) > width-2 {
					t.Fatalf("graph overflow at width %d: %q", width, line)
				}
			}
		}
	}
}

func TestTopologyDoesNotHideUnreadyOrStaleData(t *testing.T) {
	ctx := fixture(t, 2)
	ctx.Data.Pods[1].Status.ContainerStatuses[0].Ready = false
	if !strings.Contains(Topology(ctx, 118), "not ready") {
		t.Fatal("running but unready pod presented as healthy")
	}
	ctx = fixture(t, 2)
	ctx.SQLLive = false
	if strings.Contains(Topology(ctx, 118), "streaming") {
		t.Fatal("old replication sample presented as live")
	}
	ctx = fixture(t, 2)
	ctx.Data.Sql.Replication = nil
	if !strings.Contains(Topology(ctx, 118), "No matching WAL sender") || !strings.Contains(Topology(ctx, 118), "┄") {
		t.Fatal("missing replication connection is not identified")
	}
}

func TestUnknownPrimaryAndJobPods(t *testing.T) {
	ctx := fixture(t, 1)
	var job monitor.PodResource
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"postgres-backup","ownerReferences":[{"kind":"Job"}]}}`), &job); err != nil {
		t.Fatal(err)
	}
	ctx.Data.Pods = append(ctx.Data.Pods, job)
	if len(ctx.Nodes()) != 2 {
		t.Fatal("job pod shown as a database replica")
	}
	ctx.Data.Cluster.Status.CurrentPrimary = ""
	if !strings.Contains(Topology(ctx, 118), "ROLE UNKNOWN") {
		t.Fatal("missing primary is not represented as unknown")
	}
}

func TestRecoveryJobPodIsNotAReplica(t *testing.T) {
	ctx := fixture(t, 0)
	ctx.Data.Cluster.Status.CurrentPrimary = ""
	ctx.Data.Cluster.Status.TargetPrimary = "postgres-1"
	ctx.Data.Cluster.Status.Phase = "Setting up primary"
	var job monitor.PodResource
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"postgres-1-full-recovery-abc","labels":{"cnpg.io/jobRole":"full-recovery","cnpg.io/instanceName":"postgres-1"},"ownerReferences":[{"kind":"Job"}]},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}`), &job); err != nil {
		t.Fatal(err)
	}
	ctx.Data.Pods = []monitor.PodResource{job}
	nodes := ctx.Nodes()
	if len(nodes) != 1 || nodes[0].Pod == nil || nodes[0].Pod.Metadata.Name != "postgres-1-full-recovery-abc" {
		t.Fatalf("recovery pod should attach to the target primary, not appear as a replica: %+v", nodes)
	}
}
