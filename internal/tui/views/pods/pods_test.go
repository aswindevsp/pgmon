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
		raw := fmt.Sprintf(`{"metadata":{"name":"postgres-%d"},"spec":{"nodeName":"worker-%d","containers":[{"name":"postgres","resources":{"limits":{"cpu":"1","memory":"1Gi"},"requests":{"cpu":"100m","memory":"256Mi"}}}]},"status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":0}]}}`, i, i)
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
			// Each replica must receive exactly one incoming WAL flow at every size.
			if got := strings.Count(graph, "▼"); got != replicas {
				t.Errorf("replicas=%d width=%d: got %d branches\n%s", replicas, width, got, graph)
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
	broken := Topology(ctx, 118)
	if !strings.Contains(broken, "No matching WAL sender") || !strings.Contains(broken, "BROKEN") || !strings.Contains(broken, "┄") {
		t.Fatal("missing replication connection is not identified:\n", broken)
	}
}

func TestArchiveReplayReplacesBrokenSender(t *testing.T) {
	ctx := fixture(t, 1)
	ctx.Data.Sql.Replication = nil
	ctx.Data.Sql.CurrentWALLSN = "0/100000"
	ctx.Data.Sql.TimelineID = 4
	ctx.Data.ReplayProgress = map[string]monitor.WALReplayProgress{
		"postgres-2": {
			Pod: "postgres-2", State: "in archive recovery", Timeline: 4,
			Position: 512 << 10, Start: 256 << 10, Target: 1024 << 10,
			Restarts: 1, RateBytesPerSec: 1024 * 1024, HasRate: true,
		},
	}
	graph := Topology(ctx, 118)
	if !strings.Contains(graph, "catching up") || strings.Contains(graph, "BROKEN") {
		t.Fatalf("archive replay should not look broken:\n%s", graph)
	}
	ctx.SelectedPod = "postgres-2"
	details := Render(ctx, 140)
	for _, want := range []string{"WAL replay", "replaying WAL from archive", "at 0/00080000", "~512.0 KiB to consistency", "1.0 MiB/s", "ETA ≈<1s", "control-file recovery position"} {
		if !strings.Contains(details, want) {
			t.Errorf("pod details missing %q:\n%s", want, details)
		}
	}
}

func TestTopologyMarksBrokenWALFlow(t *testing.T) {
	ctx := fixture(t, 2)
	ctx.Data.Sql.Replication[0].State = "stopped"
	ctx.Data.Sql.Replication[1] = monitor.ReplicationStatus{}
	graph := Topology(ctx, 118)
	if !strings.Contains(graph, "BROKEN") || !strings.Contains(graph, "✕") || !strings.Contains(graph, "┄") {
		t.Fatal("broken WAL path should be visible:\n", graph)
	}
}

func TestTopologyPyramidPutsPrimaryAboveReplicas(t *testing.T) {
	ctx := fixture(t, 2)
	graph := Topology(ctx, 118)
	primary := strings.Index(graph, "postgres-1")
	left := strings.Index(graph, "postgres-2")
	right := strings.Index(graph, "postgres-3")
	if primary < 0 || left < primary || right < primary {
		t.Fatal("primary should sit above replicas:\n", graph)
	}
	sideBySide := false
	for _, line := range strings.Split(graph, "\n") {
		if strings.Contains(line, "postgres-2") && strings.Contains(line, "postgres-3") {
			sideBySide = true
		}
	}
	if !sideBySide {
		t.Fatal("replicas should sit side by side under the primary:\n", graph)
	}
}

func TestTopologyShowsNodeAndLiveUsage(t *testing.T) {
	ctx := fixture(t, 1)
	ctx.Data.PodUsage = map[string]monitor.ResourceUsage{
		"postgres-1": {CPUCores: 0.045, MemoryBytes: 256 << 20},
		"postgres-2": {CPUCores: 0.012, MemoryBytes: 180 << 20},
	}
	ctx.Data.PodUsageHistory = map[string]monitor.UsageHistory{
		"postgres-1": {CPU: []float64{0.02, 0.03, 0.045}, Memory: []float64{200 << 20, 220 << 20, 256 << 20}},
		"postgres-2": {CPU: []float64{0.01, 0.011, 0.012}, Memory: []float64{160 << 20, 170 << 20, 180 << 20}},
	}
	ctx.Data.NodeUsageHistory = map[string]monitor.UsageHistory{
		"worker-1": {CPU: []float64{1.4, 1.8, 2.1}},
	}
	graph := Topology(ctx, 118)
	for _, want := range []string{"worker-1", "worker-2", "45m / 1", "256Mi / 1Gi", "█", "░"} {
		if !strings.Contains(graph, want) {
			t.Errorf("missing %q:\n%s", want, graph)
		}
	}
	ctx.Data.PodUsage, ctx.Data.PodUsageHistory, ctx.Data.NodeUsage = nil, nil, nil
	ctx.Data.MetricsErr = "metrics.k8s.io unavailable"
	missing := Topology(ctx, 118)
	if !strings.Contains(missing, "unavailable") || !strings.Contains(missing, "worker-1") || !strings.Contains(missing, "/ 1") {
		t.Fatal("node and limits should remain visible when usage cannot be sampled:\n", missing)
	}
}

func TestTopologyUsesNodeCapWhenPodHasNone(t *testing.T) {
	ctx := fixture(t, 0)
	ctx.Data.Pods[0].Spec.Containers[0].Resources.Limits = nil
	ctx.Data.Pods[0].Spec.Containers[0].Resources.Requests = nil
	ctx.Data.NodeCaps = map[string]monitor.ResourceCap{
		"worker-1": {CPUCores: 8, MemoryBytes: 32 << 30, HasCPU: true, HasMemory: true},
	}
	ctx.Data.PodUsage = map[string]monitor.ResourceUsage{
		"postgres-1": {CPUCores: 0.315, MemoryBytes: 16<<30 + 300<<20},
	}
	graph := Topology(ctx, 118)
	if !strings.Contains(graph, "315m / 8") || !strings.Contains(graph, "/ 32Gi") {
		t.Fatal("pod without limits should use node allocatable:\n", graph)
	}
	if strings.Contains(graph, " / —") {
		t.Fatal("node cap should replace the missing pod limit:\n", graph)
	}
}

func TestTopologyShowsAvailableWhenPodHasNoLimits(t *testing.T) {
	ctx := fixture(t, 0)
	ctx.Data.Pods[0].Spec.Containers[0].Resources.Limits = nil
	ctx.Data.Pods[0].Spec.Containers[0].Resources.Requests = nil
	ctx.Data.NodeCaps = map[string]monitor.ResourceCap{
		"worker-1": {CPUCores: 8, MemoryBytes: 32 << 30, HasCPU: true, HasMemory: true},
	}
	ctx.Data.PodUsage = map[string]monitor.ResourceUsage{
		"postgres-1": {CPUCores: 0.5, MemoryBytes: 16 << 30},
	}
	ctx.Data.NodeUsage = map[string]monitor.ResourceUsage{
		"worker-1": {CPUCores: 2, MemoryBytes: 20 << 30},
	}
	graph := Topology(ctx, 118)
	// Available = allocatable - (nodeUsage - podUsage): CPU 8-(2-0.5)=6.5, MEM 32-(20-16)=28.
	if !strings.Contains(graph, "500m / 6.50 (8)") {
		t.Error("CPU should show available with node cap in parens:\n", graph)
	}
	if !strings.Contains(graph, "16Gi / 28Gi (32Gi)") {
		t.Error("MEM should show available with node cap in parens:\n", graph)
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
