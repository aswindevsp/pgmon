package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"postgres-monitor/internal/monitor"
)

func TestViewportBounds(t *testing.T) {
	for _, replicas := range []int{0, 1, 2, 5} {
		for _, width := range []int{32, 48, 80, 98, 120, 160} {
			m := fixture(t, replicas)
			m.width = width
			for _, height := range []int{10, 24, 40} {
				m.height = height
				for focus := 0; focus < 6; focus++ {
					m.focus = focus
					view := strings.Split(m.View(), "\n")
					if len(view) != height {
						t.Fatalf("viewport %dx%d has %d lines", width, height, len(view))
					}
					for _, line := range view {
						if lipgloss.Width(line) > width {
							t.Fatalf("viewport width %d overflow: %q", width, line)
						}
					}
				}
			}
		}
	}
}

func TestLiveSampleSurvivesInFlightRefresh(t *testing.T) {
	m := fixture(t, 1)
	m.data.SqlAt = time.Now().Add(-15 * time.Second)
	if m.sqlLive() {
		t.Fatal("aged sample must be stale when no collect is running")
	}
	m.sqlBusy = true
	if !m.sqlLive() {
		t.Fatal("aged sample hidden while SQL refresh is still running")
	}
	m.data.SqlAt = time.Now().Add(-2 * time.Minute)
	if m.sqlLive() {
		t.Fatal("hung collect presented a two-minute-old sample as live")
	}
}

func TestTickStartsSQLWhileKubeCollectRuns(t *testing.T) {
	m := fixture(t, 1)
	m.kubeBusy, m.busy = true, true
	updated, cmd := m.Update(tickMsg(time.Now()))
	m = updated.(model)
	if !m.sqlBusy || !m.kubeBusy || !m.busy {
		t.Fatal("SQL collect must start beside an in-flight Kubernetes collect")
	}
	if cmd == nil {
		t.Fatal("expected SQL collect and next tick")
	}
}

func TestTickStartsReplayCollectAndAppliesLiveCursor(t *testing.T) {
	m := fixture(t, 1)
	m.data.ReplayProgress = map[string]monitor.WALReplayProgress{"postgres-2": {Pod: "postgres-2"}}
	updated, _ := m.Update(tickMsg(time.Now()))
	m = updated.(model)
	if !m.replayBusy || !m.busy {
		t.Fatal("tick must start the live archive-replay collect")
	}
	// A second tick while the collect is running must not start another.
	updated, _ = m.Update(tickMsg(time.Now()))
	if !updated.(model).replayBusy {
		t.Fatal("in-flight replay collect must be exclusive")
	}
	live := uint64(1) << 20
	next := m.data
	next.ReplayProgress = map[string]monitor.WALReplayProgress{"postgres-2": {Pod: "postgres-2", Live: live}}
	updated, _ = m.Update(refreshMsg{data: next, replay: true})
	m = updated.(model)
	if m.replayBusy {
		t.Fatal("replay collect must clear its busy flag")
	}
	if got := m.data.ReplayProgress["postgres-2"].Live; got != live {
		t.Fatalf("replay apply dropped the live cursor sample: %d", got)
	}
}

func TestSQLRefreshDoesNotClobberKubernetesSnapshot(t *testing.T) {
	m := fixture(t, 1)
	m.sqlBusy = true
	m.data.ResourceErr = "pods: timed out"
	next := m.data
	next.Sql.Version = "17.6"
	next.ResourceErr = ""
	updated, _ := m.Update(refreshMsg{data: next, Sql: true})
	m = updated.(model)
	if m.sqlBusy || m.data.Sql.Version != "17.6" || m.data.ResourceErr != "pods: timed out" {
		t.Fatalf("SQL apply mixed snapshots: busy=%v version=%q kube=%q", m.sqlBusy, m.data.Sql.Version, m.data.ResourceErr)
	}
}

func TestSQLLivenessRejectsStaleFailedAndFailedOverSamples(t *testing.T) {
	m := fixture(t, 2)
	m.data.SqlErr = "query failed"
	if m.sqlLive() {
		t.Fatal("failed sample presented as live")
	}
	m = fixture(t, 2)
	m.data.Cluster.Status.CurrentPrimary = "postgres-2"
	if m.sqlLive() || m.viewContext().Replication("postgres-3") != nil {
		t.Fatal("previous primary's sample reused after failover")
	}
	m = fixture(t, 2)
	m.data.SqlAt = time.Now().Add(-time.Minute)
	if m.sqlLive() {
		t.Fatal("stale SQL sample presented as live")
	}
}

func TestRecoveryHeaderState(t *testing.T) {
	m := fixture(t, 0)
	m.data.Cluster.Status.CurrentPrimary = ""
	m.data.Cluster.Status.TargetPrimary = "postgres-1"
	m.data.Cluster.Status.Phase = "Setting up primary"
	if !strings.Contains(m.View(), "RECOVERING") {
		t.Fatal("recovery phase not shown in header:", m.View())
	}
}

func TestLongValuesAndErrorsStayInsideViewport(t *testing.T) {
	m := fixture(t, 2)
	m.width, m.height = 48, 18
	m.cfg.Context = strings.Repeat("very-long-context-", 10)
	m.data.ResourceErr = strings.Repeat("permission denied\n", 12)
	m.data.Pods[1].Spec.NodeName = strings.Repeat("worker-", 20)
	m.data.Pods[1].Metadata.Name = strings.Repeat("数据库", 12)
	for focus := 0; focus < 6; focus++ {
		m.focus, m.scroll = focus, 1<<20
		m.clampScroll()
		lines := strings.Split(m.View(), "\n")
		if len(lines) != m.height {
			t.Fatal("long data changed viewport height")
		}
		for _, line := range lines {
			if lipgloss.Width(line) > m.width {
				t.Fatalf("long data overflow: %q", line)
			}
		}
	}
}
