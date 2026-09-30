package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"postgres-monitor/internal/monitor"
)

func TestQueryFilterAndSortKeys(t *testing.T) {
	m := fixture(t, 1)
	m.focus = 3
	m.data.Sql.TopQueries = []monitor.QueryStat{{Query: "SELECT orders FROM shop", Calls: 1, TotalExecTime: 100}, {Query: "SELECT payments FROM billing", Calls: 10, TotalExecTime: 5}}
	m.data.Sql.Sessions = []monitor.SessionStatus{{PID: 10, State: "idle in transaction", Query: "UPDATE shop SET quantity=1"}, {PID: 20, State: "active", Query: "SELECT * FROM shop", BlockingPIDs: []int{10}}}
	if m.viewContext().SortCalls {
		t.Fatal("default order should use total execution time")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = updated.(model)
	if !m.viewContext().SortCalls {
		t.Fatal("calls sort not applied")
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("/")}, {Type: tea.KeyRunes, Runes: []rune("orders")}, {Type: tea.KeyEnter}} {
		updated, _ = m.Update(key)
		m = updated.(model)
	}
	if m.filter != "orders" {
		t.Fatal("filter not applied")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if !strings.Contains(m.View(), "SELECT orders FROM shop") || !m.detail {
		t.Fatal("query details missing")
	}
}

func TestMutationKeysDoNothing(t *testing.T) {
	for focus := 0; focus < 6; focus++ {
		m := fixture(t, 1)
		m.focus = focus
		for _, key := range []string{"b", "c", "x", "X", "d", "e"} {
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			if cmd != nil || updated.(model).busy {
				t.Fatalf("mutation shortcut %s dispatched command", key)
			}
		}
	}
}
