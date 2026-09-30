package shared

import "github.com/charmbracelet/lipgloss"

var (
	Muted       = lipgloss.NewStyle().Foreground(lipgloss.Color("#8190A5"))
	Bright      = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6EDF7"))
	Cyan        = lipgloss.NewStyle().Foreground(lipgloss.Color("#55D6E8"))
	Green       = lipgloss.NewStyle().Foreground(lipgloss.Color("#53D7A2"))
	Amber       = lipgloss.NewStyle().Foreground(lipgloss.Color("#F4BD5E"))
	Red         = lipgloss.NewStyle().Foreground(lipgloss.Color("#F17979"))
	PanelTitle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#55D6E8"))
	PanelBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("#344155"))
	ActiveTab   = lipgloss.NewStyle().Foreground(lipgloss.Color("#111B29")).Background(lipgloss.Color("#55D6E8")).Bold(true)
	CardBorder  = lipgloss.Border{Top: "─", Bottom: "─", Left: "│", Right: "│", TopLeft: "╭", TopRight: "╮", BottomLeft: "╰", BottomRight: "╯"}
)
