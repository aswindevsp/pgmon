package shared

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDashboardTripleWideHeightMatches(t *testing.T) {
	out := DashboardTriple("Load", []string{"l1", "l2", "l3", "l4", "l5"}, "WAL", []string{"w1", "w2"}, "Attention", []string{"a1"}, 150)
	lines := strings.Split(out, "\n")
	if len(lines) != 7 {
		t.Fatalf("expected all panels to share the tallest height, got %d lines:\n%s", len(lines), out)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 150 {
			t.Errorf("line %d width = %d, want 150", i, w)
		}
	}
	for _, want := range []string{"Load", "WAL", "Attention", "l5", "w2", "a1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestDashboardTripleNarrowStacks(t *testing.T) {
	out := DashboardTriple("Load", []string{"l1"}, "WAL", []string{"w1"}, "Attention", []string{"a1"}, 100)
	if strings.Count(out, "╭") != 3 {
		t.Fatalf("expected three stacked panels:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w != 100 {
			t.Fatalf("stacked panel line width = %d, want 100", w)
		}
	}
}

func TestColoredMeterUsesFillStyle(t *testing.T) {
	got := ansi.Strip(ColoredMeter(0.5, 10, Red))
	if got != strings.Repeat("█", 5)+strings.Repeat("░", 5) {
		t.Fatalf("ColoredMeter = %q", got)
	}
}
