package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"postgres-monitor/internal/monitor"
)

func TestSwitchoverKeyOpensConfirmOnReplica(t *testing.T) {
	m := fixture(t, 1)
	m.focus = 5
	m.selectedPod = "postgres-2"
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("opening confirm must not dispatch")
	}
	if m.switchoverConfirm == nil || m.switchoverConfirm.primary != "postgres-1" || m.switchoverConfirm.target != "postgres-2" {
		t.Fatalf("p must open switchover confirm: %+v", m.switchoverConfirm)
	}
	if !strings.Contains(m.View(), "Confirm switchover") || !strings.Contains(m.View(), "postgres-2") {
		t.Fatal("confirm panel missing:\n", m.View())
	}
}

func TestSwitchoverKeyOnPrimaryShowsReason(t *testing.T) {
	m := fixture(t, 1)
	m.focus = 0
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(model)
	if m.switchoverConfirm != nil || cmd != nil {
		t.Fatal("primary must not open confirm")
	}
	if !strings.Contains(m.switchoverNotice, "current primary") {
		t.Fatalf("must explain primary rejection: %q", m.switchoverNotice)
	}
}

func TestSwitchoverIgnoredOffInstanceTabs(t *testing.T) {
	m := fixture(t, 1)
	m.focus = 3
	m.selectedPod = "postgres-2"
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(model)
	if m.switchoverConfirm != nil || m.switchoverNotice != "" || cmd != nil {
		t.Fatal("switchover key must stay on Overview and Pods")
	}
}

func TestSwitchoverConfirmKeys(t *testing.T) {
	m := fixture(t, 1)
	m.focus = 5
	m.selectedPod = "postgres-2"
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(model)
	for _, k := range []string{"tab", "q", "r", "p"} {
		var key tea.KeyMsg
		if k == "tab" {
			key = tea.KeyMsg{Type: tea.KeyTab}
		} else {
			key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		updated, cmd := m.Update(key)
		nm := updated.(model)
		if nm.focus != 5 || nm.switchoverConfirm == nil || cmd != nil {
			t.Fatalf("key %q must be inactive during switchover confirm", k)
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.switchoverConfirm != nil {
		t.Fatal("esc must close confirm")
	}
}

func TestSwitchoverConfirmDispatchesPromote(t *testing.T) {
	m := fixture(t, 1)
	m.focus = 5
	m.selectedPod = "postgres-2"
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(model)
	if cmd == nil || !m.switchoverBusy || m.switchoverConfirm != nil {
		t.Fatal("y must dispatch promote and mark busy")
	}
	if !strings.Contains(m.switchoverNotice, "postgres-1") || !strings.Contains(m.switchoverNotice, "postgres-2") {
		t.Fatalf("request notice missing pair: %q", m.switchoverNotice)
	}
}

func TestOverviewJKSelectsInstance(t *testing.T) {
	m := fixture(t, 1)
	m.focus = 0
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.selectedPod != "postgres-2" {
		t.Fatalf("overview down should select replica, got %q", m.selectedPod)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(model)
	if m.selectedPod != "postgres-1" {
		t.Fatalf("overview up should return to primary, got %q", m.selectedPod)
	}
}

func TestSwitchoverResultAndProgress(t *testing.T) {
	m := fixture(t, 1)
	updated, _ := m.Update(switchoverResult{primary: "postgres-1", target: "postgres-2"})
	m = updated.(model)
	if m.switchoverPending != "postgres-2" || !strings.Contains(m.switchoverNotice, "waiting") {
		t.Fatalf("success must wait for currentPrimary: %q pending=%q", m.switchoverNotice, m.switchoverPending)
	}
	next := m.data
	next.Cluster.Status.TargetPrimary = "postgres-2"
	updated, _ = m.Update(refreshMsg{data: next, Sql: true})
	m = updated.(model)
	if !strings.Contains(m.switchoverNotice, "in progress") {
		t.Fatalf("target change should report progress: %q", m.switchoverNotice)
	}
	next.Cluster.Status.CurrentPrimary = "postgres-2"
	updated, _ = m.Update(refreshMsg{data: next, Sql: true})
	m = updated.(model)
	if m.switchoverPending != "" || !strings.Contains(m.switchoverNotice, "Primary is now postgres-2") {
		t.Fatalf("completed switchover not observed: pending=%q notice=%q", m.switchoverPending, m.switchoverNotice)
	}
}

func TestSwitchoverFailureClearsPending(t *testing.T) {
	m := fixture(t, 1)
	m.switchoverPending = "postgres-2"
	updated, _ := m.Update(switchoverResult{primary: "postgres-1", target: "postgres-2", err: errorString("plugin missing")})
	m = updated.(model)
	if m.switchoverPending != "" || !strings.Contains(m.switchoverNotice, "Switchover failed") {
		t.Fatalf("failure must clear pending: %q %q", m.switchoverPending, m.switchoverNotice)
	}
}

func TestSwitchoverRefreshKeepsConfirmOpen(t *testing.T) {
	m := fixture(t, 1)
	m.switchoverConfirm = &switchoverConfirm{primary: "postgres-1", target: "postgres-2"}
	next := m.data
	updated, _ := m.Update(refreshMsg{data: next, kube: true})
	m = updated.(model)
	if m.switchoverConfirm == nil || m.switchoverConfirm.target != "postgres-2" {
		t.Fatal("confirm must survive refresh")
	}
}

func TestBlockedSwitchoverDoesNotDispatch(t *testing.T) {
	m := fixture(t, 1)
	m.focus = 5
	m.selectedPod = "postgres-2"
	m.data.Cluster.Status.TargetPrimary = "postgres-2"
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(model)
	if m.switchoverConfirm != nil || cmd != nil {
		t.Fatal("in-progress switchover must not confirm")
	}
	if !strings.Contains(m.switchoverNotice, "in progress") {
		t.Fatalf("must explain in-progress: %q", m.switchoverNotice)
	}
}

func TestEmptyClusterIsNotInProgress(t *testing.T) {
	var c monitor.ClusterResource
	if monitor.SwitchoverInProgress(c) {
		t.Fatal("empty current/target is not a switchover")
	}
	c.Status.CurrentPrimary, c.Status.TargetPrimary = "a", "b"
	if !monitor.SwitchoverInProgress(c) {
		t.Fatal("expected in progress")
	}
}
