package shared

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pgmon/internal/monitor"
)

func switchoverCtx(t *testing.T) Context {
	t.Helper()
	ctx := Context{Started: true, SQLLive: true, KubeEvery: 15 * time.Second, Namespace: "cnpg", Cluster: "postgres", SelectedPod: "postgres-2"}
	ctx.Data.KubeAt = time.Now()
	ctx.Data.Cluster.Status.CurrentPrimary = "postgres-1"
	ctx.Data.Cluster.Status.TargetPrimary = "postgres-1"
	for _, name := range []string{"postgres-1", "postgres-2"} {
		var p monitor.PodResource
		raw := `{"metadata":{"name":"` + name + `"},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}`
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		ctx.Data.Pods = append(ctx.Data.Pods, p)
	}
	return ctx
}

func TestSwitchoverActionLinesShowPlan(t *testing.T) {
	ctx := switchoverCtx(t)
	text := strings.Join(SwitchoverActionLines(ctx, 100), "\n")
	for _, want := range []string{"postgres-2", "replica", "postgres-1", "p switchover", "kubectl cnpg promote"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
}

func TestSwitchoverActionLinesExplainPrimary(t *testing.T) {
	ctx := switchoverCtx(t)
	ctx.SelectedPod = "postgres-1"
	text := strings.Join(SwitchoverActionLines(ctx, 80), "\n")
	if !strings.Contains(text, "unavailable") || !strings.Contains(text, "current primary") {
		t.Fatalf("primary must be explained: %s", text)
	}
}

func TestSwitchoverConfirmPanelNamesPair(t *testing.T) {
	ctx := switchoverCtx(t)
	ctx.SwitchoverConfirmPrimary = "postgres-1"
	ctx.SwitchoverConfirmTarget = "postgres-2"
	title, lines := SwitchoverConfirmPanel(ctx)
	text := title + "\n" + strings.Join(lines, "\n")
	for _, want := range []string{"Confirm switchover", "postgres-2", "postgres-1", "y/Enter", "Esc"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
}
