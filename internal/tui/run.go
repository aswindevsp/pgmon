package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"postgres-monitor/internal/monitor"
)

// Run starts cluster discovery and the terminal dashboard.
func Run(cfg monitor.Config) error {
	_, err := tea.NewProgram(discoveryModel{cfg: cfg, loading: true}, tea.WithAltScreen()).Run()
	return err
}
