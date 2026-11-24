package pods

import (
	"strings"
	"testing"
	"time"
)

func TestRenderShowsSwitchoverActionAndConfirm(t *testing.T) {
	ctx := fixture(t, 1)
	ctx.SelectedPod = "postgres-2"
	ctx.Namespace = "cnpg"
	ctx.Cluster = "postgres"
	text := Render(ctx, 100)
	for _, want := range []string{"Switchover", "p switchover", "postgres-2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	ctx.SwitchoverConfirmPrimary = "postgres-1"
	ctx.SwitchoverConfirmTarget = "postgres-2"
	text = Render(ctx, 100)
	if !strings.Contains(text, "Confirm switchover") || !strings.Contains(text, "y/Enter") {
		t.Fatalf("confirm missing:\n%s", text)
	}
}

func TestRenderShowsSwitchoverNotice(t *testing.T) {
	ctx := fixture(t, 1)
	ctx.Data.KubeAt = time.Now()
	ctx.SwitchoverNotice = "Switchover failed: forbidden"
	text := Render(ctx, 80)
	if !strings.Contains(text, "Switchover failed") {
		t.Fatalf("notice missing:\n%s", text)
	}
}
