package tui

import (
	"postgres-monitor/internal/tui/views/pods"
	"postgres-monitor/internal/tui/views/shared"
)

func (m model) viewportSize() (int, int, int) {
	w, h := m.width, m.height
	if w == 0 {
		w = 100
	}
	if h == 0 {
		h = 32
	}
	margin := 1
	if w >= 80 {
		margin = 2
	}
	return w - 2*margin, max(1, h-9), margin
}

// movePod selects the next or previous pod and scrolls the topology into view.
func (m *model) movePod(delta int) {
	nodes := m.viewContext().Nodes()
	i := (shared.SelectedIndex(nodes, m.selectedPod) + delta + len(nodes)) % len(nodes)
	m.selectedPod = nodes[i].Name
	target := 4 + i*(pods.NodeHeight+1) + pods.NodeHeight/2
	if m.width-2 >= 96 && len(nodes) > 1 {
		target = 4 + (i-1)*(pods.NodeHeight+1) + pods.NodeHeight/2
		if i == 0 {
			target = 4 + ((len(nodes)-1)*(pods.NodeHeight+1)-1)/2
		}
	}
	m.scroll = max(0, target-max(1, m.height-6)/2)
}

func (m *model) clampScroll() {
	if (m.width > 0 && m.width < 32) || (m.height > 0 && m.height < 10) {
		m.scroll = 0
		return
	}
	width, height, _ := m.viewportSize()
	m.scroll = min(max(0, m.scroll), max(0, len(m.bodyLines(width))-height))
}
