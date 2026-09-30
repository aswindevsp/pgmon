package tui

import (
	"errors"
	"fmt"
	"postgres-monitor/internal/monitor"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const discoveredClusters = `{"items":[
 {"metadata":{"name":"database","namespace":"production"}},
 {"metadata":{"name":"database","namespace":"development"}}
]}`

func TestDiscoveryUsesCurrentContextAcrossNamespaces(t *testing.T) {
	var calls [][]string
	result := monitor.Discover(monitor.Config{}, func(args ...string) ([]byte, error) {
		calls = append(calls, args)
		if len(calls) == 1 {
			return []byte("my-current-context\n"), nil
		}
		return []byte(discoveredClusters), nil
	})
	want := [][]string{{"config", "current-context"}, {"--context", "my-current-context", "get", "clusters.postgresql.cnpg.io", "--all-namespaces", "-o", "json"}}
	if result.Err != nil || !reflect.DeepEqual(calls, want) {
		t.Fatalf("discovery: calls=%v error=%v", calls, result.Err)
	}
	if result.Config.Context != "my-current-context" || len(result.Clusters) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Clusters[0].Metadata.Namespace != "development" {
		t.Fatal("clusters are not sorted by namespace")
	}
}

func TestDiscoveryOverridesAndFilters(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      monitor.Config
		want     []string
		response string
		count    int
	}{
		{"context", monitor.Config{Context: "custom"}, []string{"--context", "custom", "get", "clusters.postgresql.cnpg.io", "--all-namespaces", "-o", "json"}, discoveredClusters, 2},
		{"namespace", monitor.Config{Context: "custom", Namespace: "development"}, []string{"--context", "custom", "get", "clusters.postgresql.cnpg.io", "--namespace", "development", "-o", "json"}, `{"items":[{"metadata":{"name":"database","namespace":"development"}}]}`, 1},
		{"exact", monitor.Config{Context: "custom", Namespace: "production", Cluster: "database"}, []string{"--context", "custom", "get", "clusters.postgresql.cnpg.io", "database", "--namespace", "production", "-o", "json"}, `{"metadata":{"name":"database","namespace":"production"}}`, 1},
		{"same name across namespaces", monitor.Config{Context: "custom", Cluster: "database"}, []string{"--context", "custom", "get", "clusters.postgresql.cnpg.io", "--all-namespaces", "-o", "json"}, discoveredClusters, 2},
		{"missing cluster", monitor.Config{Context: "custom", Cluster: "absent"}, []string{"--context", "custom", "get", "clusters.postgresql.cnpg.io", "--all-namespaces", "-o", "json"}, discoveredClusters, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := monitor.Discover(tc.cfg, func(args ...string) ([]byte, error) {
				if !reflect.DeepEqual(args, tc.want) {
					t.Fatalf("args=%v want=%v", args, tc.want)
				}
				return []byte(tc.response), nil
			})
			if result.Err != nil || len(result.Clusters) != tc.count {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestDiscoveryErrorsStayOnStartupScreen(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		err            error
	}{
		{"denied", "", errors.New("Forbidden")},
		{"invalid JSON", "not json", nil},
		{"missing namespace", `{"items":[{"metadata":{"name":"database"}}]}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := monitor.Discover(monitor.Config{Context: "custom"}, func(...string) ([]byte, error) { return []byte(tc.response), tc.err })
			if result.Err == nil {
				t.Fatal("expected discovery error")
			}
			updated, cmd := (discoveryModel{loading: true}).Update(result)
			m, ok := updated.(discoveryModel)
			if !ok || cmd != nil || !strings.Contains(m.View(), "Unable to discover") {
				t.Fatal("failed discovery did not stay on startup screen")
			}
			_, retry := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
			if retry == nil {
				t.Fatal("retry did not start discovery")
			}
		})
	}
	result := monitor.Discover(monitor.Config{}, func(...string) ([]byte, error) { return []byte("\n"), nil })
	if result.Err == nil || !strings.Contains(result.Err.Error(), "no current") {
		t.Fatal("empty current context was not handled")
	}
}

func TestClusterSelectionStartsMonitoringOnlyWhenResolved(t *testing.T) {
	result := monitor.Discover(monitor.Config{Context: "current"}, func(...string) ([]byte, error) { return []byte(discoveredClusters), nil })
	initial := discoveryModel{loading: true, width: 80, height: 24}
	updated, cmd := initial.Update(result)
	m, ok := updated.(discoveryModel)
	if !ok || cmd != nil {
		t.Fatal("multiple clusters must wait for selection")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(discoveryModel)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app, ok := updated.(model)
	if !ok || cmd == nil || app.cfg != (monitor.Config{Context: "current", Namespace: "production", Cluster: "database"}) {
		t.Fatalf("wrong monitor target: %+v", updated)
	}
	if app.width != 80 || app.height != 24 || !app.busy {
		t.Fatal("terminal size or loading state lost")
	}
	result.Clusters = result.Clusters[:1]
	updated, cmd = initial.Update(result)
	if _, ok := updated.(model); !ok || cmd == nil {
		t.Fatal("single cluster did not open automatically")
	}
	result.Clusters = nil
	updated, cmd = initial.Update(result)
	if _, ok := updated.(discoveryModel); !ok || cmd != nil || !strings.Contains(updated.View(), "No CNPG clusters") {
		t.Fatal("empty list was not handled")
	}
	updated, cmd = initial.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok := updated.(discoveryModel); !ok || cmd != nil {
		t.Fatal("Enter during discovery must not open a monitor")
	}
}

func TestClusterPickerScrollsWithinTerminal(t *testing.T) {
	m := discoveryModel{cfg: monitor.Config{Context: "current"}}
	for i := 0; i < 40; i++ {
		var c monitor.ClusterResource
		c.Metadata.Namespace, c.Metadata.Name = "production", fmt.Sprintf("database-%02d", i)
		m.clusters = append(m.clusters, c)
	}
	for _, width := range []int{32, 80, 120} {
		for _, height := range []int{10, 24, 40} {
			m.width, m.height = width, height
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
			m = updated.(discoveryModel)
			view := m.View()
			if !strings.Contains(view, "database-39") {
				t.Fatal("selected cluster scrolled out of view")
			}
			lines := strings.Split(view, "\n")
			if len(lines) != height {
				t.Fatalf("height=%d got=%d", height, len(lines))
			}
			for _, line := range lines {
				if lipgloss.Width(line) > width {
					t.Fatalf("line exceeds width %d: %q", width, line)
				}
			}
		}
	}
}
