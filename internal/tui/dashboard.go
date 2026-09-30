package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/backups"
	"postgres-monitor/internal/tui/views/locks"
	"postgres-monitor/internal/tui/views/overview"
	"postgres-monitor/internal/tui/views/pods"
	"postgres-monitor/internal/tui/views/queries"
	"postgres-monitor/internal/tui/views/replication"
	"postgres-monitor/internal/tui/views/shared"
)

func (m model) View() string {
	w, h := m.width, m.height
	if w == 0 {
		w = 100
	}
	if h == 0 {
		h = 32
	}
	if w < 32 || h < 10 {
		return shared.FitLine("pgmon · enlarge terminal to 32×10", w)
	}
	width, vh, margin := m.viewportSize()
	state := shared.Muted.Render("○ connecting")
	if m.sqlLive() {
		state = shared.Green.Render("● live")
	} else if monitor.ClusterRecovering(m.data) && m.started {
		state = shared.Amber.Render("● recovering")
	} else if m.started {
		state = shared.Amber.Render("● stale")
	}
	if m.busy {
		state += " ↻"
	}
	brand := shared.Cyan.Bold(true).Render("pgmon") + shared.Muted.Render("  /  ") + shared.Bright.Bold(true).Render(m.cfg.Cluster)
	header := shared.AlignedEnds(brand, state, width)
	context := shared.Muted.Render("namespace ") + shared.Bright.Render(m.cfg.Namespace) + shared.Muted.Render("   ·   context "+m.cfg.Context+"   ·   PostgreSQL "+shared.ValueOr(m.data.Sql.Version, "—"))
	body := m.bodyLines(width)
	start := min(m.scroll, max(0, len(body)-vh))
	end := min(len(body), start+vh)
	visible := append([]string(nil), body[start:end]...)
	for len(visible) < vh {
		visible = append(visible, "")
	}
	lines := []string{"", header, context, "", m.tabs(width), ""}
	lines = append(lines, visible...)
	footer := "1–6 / tab  switch    ↑↓  move    r  refresh    ?  help    q  quit"
	if width < 75 {
		footer = "1–6 tabs   ↑↓ move   r refresh   ? help   q quit"
	}
	if m.filtering {
		footer = "Filter  " + m.filter + "▏    enter apply · esc finish"
	} else if m.filter != "" {
		footer = "Filter  " + m.filter + "    esc clear · / edit · ? help"
	}
	status := fmt.Sprintf("READ ONLY   ·   SQL 5s / K8s 15s   ·   updated %s", shared.Relative(m.data.KubeAt))
	if width < 70 {
		status = "READ ONLY · updated " + shared.Relative(m.data.KubeAt)
	}
	position := ""
	if len(body) > vh {
		position = fmt.Sprintf("%d–%d / %d  ·  PgUp/PgDn", start+1, end, len(body))
	}
	lines = append(lines, "", shared.Muted.Render(footer), shared.Muted.Render(shared.AlignedEnds(status, position, width)))
	for i := range lines {
		lines[i] = strings.Repeat(" ", margin) + shared.FitLine(strings.ReplaceAll(lines[i], "\r", " "), width)
	}
	return lipgloss.NewStyle().Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

func (m model) tabs(width int) string {
	names := []string{"Overview", "Replication", "Backups", "Queries", "Locks", "Pods"}
	var tabs []string
	for i, name := range names {
		if width < 80 {
			name = []string{"Over", "Repl", "Back", "Query", "Locks", "Pods"}[i]
		}
		if width < 58 {
			name = ""
			if i == m.focus && width >= 42 {
				name = names[i]
			}
		}
		label := fmt.Sprintf(" %d", i+1)
		if name != "" {
			label += " " + name
		}
		label += " "
		if i == m.focus {
			label = shared.ActiveTab.Render(label)
		} else {
			label = shared.Muted.Render(label)
		}
		tabs = append(tabs, label)
	}
	return shared.FitLine(strings.Join(tabs, " "), width)
}

func (m model) bodyLines(width int) []string {
	ctx := m.viewContext()
	var body string
	if m.help {
		body = shared.DashboardPanel("Help · read-only monitor", []string{
			"1 Overview   2 Replication   3 Backups   4 Queries   5 Locks   6 Pods",
			"Tab / Shift+Tab   next / previous tab", "j/k or ↑/↓        move rows; choose a pod on Pods",
			"Enter            query text / selected pod details", "/                filter queries and sessions; Enter applies", "s                sort statements by calls / total execution time",
			"PgUp/PgDn        scroll panel content", "Home / End       top / bottom", "r                refresh observations", "Esc              clear filter and close details", "?                toggle help", "q / Ctrl+C       quit",
			"All database probes run read-only. No backup, slot, query, or cluster mutation actions.",
		}, width)
	} else {
		switch m.focus {
		case 0:
			body = overview.Render(ctx, width)
		case 1:
			body = replication.Render(ctx, width)
		case 2:
			body = backups.Render(ctx, width)
		case 3:
			body = queries.Render(ctx, width)
		case 4:
			body = locks.Render(ctx, width)
		case 5:
			body = pods.Render(ctx, width)
		}
	}
	warnings := []string{}
	if m.data.ResourceErr != "" {
		warnings = append(warnings, shared.Amber.Render("Kubernetes: "+m.data.ResourceErr))
	}
	if m.data.SqlErr != "" {
		warnings = append(warnings, shared.Amber.Render("PostgreSQL: "+m.data.SqlErr))
	}
	if len(warnings) > 0 {
		body = shared.StackDashboards(shared.DashboardPanel("Collection status · retained data may be stale", warnings, width), body)
	}
	return strings.Split(body, "\n")
}
