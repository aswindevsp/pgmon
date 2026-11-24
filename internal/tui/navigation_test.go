package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"pgmon/internal/tui/views/shared"
)

func TestNavigationRevealsPanelsAndSelectedPods(t *testing.T) {
	m := fixture(t, 5)
	m.height = 24
	labels := []string{"Instances", "WAL flow", "Running", "Top statements", "Blocking chain", "Selected pod"}
	for i, label := range labels {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(fmt.Sprint(i + 1))})
		m = updated.(model)
		if m.focus != i || m.scroll != 0 || !strings.Contains(strings.Join(m.bodyLines(118), "\n"), label) {
			t.Fatalf("tab %d did not show %s", i+1, label)
		}
	}
	for i := 0; i < 5; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}
	if m.selectedPod != "postgres-6" || !strings.Contains(m.View(), "postgres-6") {
		t.Fatal("selected pod not visible")
	}
	m.data.Cluster.Status.CurrentPrimary = "postgres-6"
	if shared.SelectedIndex(m.viewContext().Nodes(), m.selectedPod) != 0 {
		t.Fatal("selection lost after primary changed")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if m.focus != 0 {
		t.Fatal("tab did not wrap")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)
	if m.focus != 5 {
		t.Fatal("shift tab did not wrap")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(model)
	if m.scroll == 0 {
		t.Fatal("page down did not scroll")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(model)
	if m.scroll != 0 {
		t.Fatal("page up did not return to top")
	}
}
