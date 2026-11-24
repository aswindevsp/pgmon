package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"pgmon/internal/monitor"
)

// Run starts cluster discovery and the terminal dashboard.
func Run(cfg monitor.Config) error {
	_, err := tea.NewProgram(discoveryModel{cfg: cfg, loading: true}, tea.WithAltScreen()).Run()
	return err
}
