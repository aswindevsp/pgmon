package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

type discoveryModel struct {
	cfg           monitor.Config
	clusters      []monitor.ClusterResource
	cursor        int
	width, height int
	loading       bool
	err           error
}

func discoverCmd(cfg monitor.Config) tea.Cmd {
	return func() tea.Msg { return monitor.Discover(cfg, nil) }
}

func (m discoveryModel) Init() tea.Cmd { return discoverCmd(m.cfg) }

func (m discoveryModel) openCluster(index int) (tea.Model, tea.Cmd) {
	c := m.clusters[index]
	cfg := m.cfg
	cfg.Namespace, cfg.Cluster = c.Metadata.Namespace, c.Metadata.Name
	next := model{cfg: cfg, width: m.width, height: m.height, busy: true}
	return next, next.Init()
}

func (m discoveryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case monitor.DiscoveryResult:
		m.cfg, m.clusters, m.err = msg.Config, msg.Clusters, msg.Err
		m.loading, m.cursor = false, 0
		if msg.Err == nil && len(msg.Clusters) == 1 {
			return m.openCluster(0)
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading, m.err = true, nil
				return m, discoverCmd(m.cfg)
			}
		case "down", "j", "tab":
			if !m.loading && len(m.clusters) > 0 {
				m.cursor = (m.cursor + 1) % len(m.clusters)
			}
		case "up", "k", "shift+tab":
			if !m.loading && len(m.clusters) > 0 {
				m.cursor = (m.cursor + len(m.clusters) - 1) % len(m.clusters)
			}
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = max(0, len(m.clusters)-1)
		case "enter":
			if !m.loading && m.err == nil && len(m.clusters) > 0 {
				return m.openCluster(m.cursor)
			}
		}
	}
	return m, nil
}

func (m discoveryModel) View() string {
	w, h := m.width, m.height
	if w == 0 {
		w = 100
	}
	if h == 0 {
		h = 24
	}
	if w < 32 || h < 10 {
		return shared.FitLine("pgwatch · enlarge terminal to 32×10", w)
	}
	width := w - 2
	scope := "all namespaces"
	if m.cfg.Namespace != "" {
		scope = m.cfg.Namespace
	}
	lines := []string{
		shared.Cyan.Bold(true).Render("pg") + shared.Bright.Bold(true).Render("watch") + shared.Muted.Render(" / SELECT CLUSTER"),
		shared.Muted.Render(shared.ValueOr(m.cfg.Context, "current kubectl context") + " / " + scope),
		shared.Muted.Render(strings.Repeat("─", width)),
	}
	var body []string
	switch {
	case m.loading:
		body = []string{shared.Cyan.Render("◌ Discovering CloudNativePG clusters…")}
	case m.err != nil:
		body = []string{shared.Amber.Render("Unable to discover CNPG clusters."), ansi.Hardwrap(m.err.Error(), width, true), "", "Check kubectl access, then press r to retry."}
		if m.cfg.Namespace == "" {
			body = append(body, "Use -namespace <name> if access is limited to a namespace.")
		}
	case len(m.clusters) == 0:
		body = []string{shared.Amber.Render("No CNPG clusters found in this scope.")}
		if m.cfg.Cluster != "" {
			body = append(body, "Requested cluster: "+m.cfg.Cluster)
		}
		body = append(body, "Check the context or namespace, then press r to retry.")
	default:
		body = []string{shared.Bright.Render(fmt.Sprintf("%d clusters found · choose one to monitor", len(m.clusters))), ""}
		visibleCount := max(1, (h-9)/3)
		start := max(0, min(m.cursor-visibleCount/2, len(m.clusters)-visibleCount))
		end := min(len(m.clusters), start+visibleCount)
		for i := start; i < end; i++ {
			c := m.clusters[i]
			marker, style := "  ", shared.Bright
			if i == m.cursor {
				marker, style = "▸ ", shared.Cyan.Bold(true)
			}
			body = append(body, style.Render(marker+c.Metadata.Namespace+" / "+c.Metadata.Name), shared.Muted.Render(fmt.Sprintf("  %d/%d ready · %s", c.Status.ReadyInstances, c.Spec.Instances, shared.ValueOr(c.Status.Phase, "phase unknown"))), "")
		}
		body = append(body, shared.Muted.Render(fmt.Sprintf("Showing %d–%d of %d", start+1, end, len(m.clusters))))
	}
	body = strings.Split(strings.Join(body, "\n"), "\n")
	bodyHeight := h - 5
	if len(body) > bodyHeight {
		body = body[:bodyHeight]
	}
	for len(body) < bodyHeight {
		body = append(body, "")
	}
	lines = append(lines, body...)
	lines = append(lines, shared.Muted.Render(strings.Repeat("─", width)), shared.Muted.Render("↑↓/tab select · enter open · r retry · q quit"))
	for i := range lines {
		lines[i] = " " + shared.FitLine(lines[i], width)
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
