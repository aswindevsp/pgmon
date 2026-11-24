package shared

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// DashboardInnerWidth returns the usable width inside a panel frame.
func DashboardInnerWidth(width int) int {
	width = max(6, width)
	inside := width - 2
	pad := 1
	if width >= 54 {
		pad = 2
	}
	return max(0, inside-2*pad)
}

// DashboardPanel draws a titled panel around lines at the requested width.
func DashboardPanel(title string, lines []string, width int) string {
	width = max(6, width)
	inside := width - 2
	pad := 1
	if width >= 54 {
		pad = 2
	}
	label := FitLine(" "+title+" ", inside-1)
	top := PanelBorder.Render("╭─") + PanelTitle.Bold(true).Render(label) + PanelBorder.Render(strings.Repeat("─", max(0, inside-1-lipgloss.Width(label)))+"╮")
	out := []string{top}
	for _, line := range lines {
		for _, part := range strings.Split(line, "\n") {
			part = FitLine(part, max(0, inside-2*pad))
			out = append(out, PanelBorder.Render("│")+strings.Repeat(" ", pad)+part+strings.Repeat(" ", max(0, inside-pad-lipgloss.Width(part)))+PanelBorder.Render("│"))
		}
	}
	out = append(out, PanelBorder.Render("╰"+strings.Repeat("─", inside)+"╯"))
	return strings.Join(out, "\n")
}

// StackDashboards joins non-empty panels vertically.
func StackDashboards(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "\n")
}

func longestLineWidth(lines []string) int {
	w := 0
	for _, line := range lines {
		w = max(w, lipgloss.Width(line))
	}
	return w
}

// JoinSideColumns places left and right content side by side within total width.
func JoinSideColumns(left, right []string, total int) []string {
	mid := "  " + Muted.Render("│") + "  "
	midW := lipgloss.Width(mid)
	avail := max(8, total-midW)
	needed := longestLineWidth(left)
	leftWidth := min(max(needed, avail/2), max(28, avail-36))
	rightWidth := max(8, avail-leftWidth)
	n := max(len(left), len(right))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(left) {
			l = FitLine(left[i], leftWidth)
		}
		if i < len(right) {
			r = FitLine(right[i], rightWidth)
		}
		out = append(out, l+strings.Repeat(" ", max(0, leftWidth-lipgloss.Width(l)))+mid+r)
	}
	return out
}

// DashboardPair lays two panels side by side when wide, otherwise stacks them.
func DashboardPair(lt string, left []string, rt string, right []string, width int) string {
	if width < 110 {
		return StackDashboards(DashboardPanel(lt, left, width), DashboardPanel(rt, right, width))
	}
	// Balance actual rendered lines, including multiline values.
	l := strings.Split(strings.Join(left, "\n"), "\n")
	r := strings.Split(strings.Join(right, "\n"), "\n")
	height := max(len(l), len(r))
	for len(l) < height {
		l = append(l, "")
	}
	for len(r) < height {
		r = append(r, "")
	}
	w := (width - 3) / 2
	return lipgloss.JoinHorizontal(lipgloss.Top, DashboardPanel(lt, l, w), "   ", DashboardPanel(rt, r, width-w-3))
}

// DashboardTriple lays three panels side by side when wide, otherwise stacks
// them. All three panels share the tallest panel's height so their frames line
// up; empty rows are drawn with borders, never as fake data.
func DashboardTriple(t1 string, l1 []string, t2 string, l2 []string, t3 string, l3 []string, width int) string {
	if width < 110 {
		return StackDashboards(DashboardPanel(t1, l1, width), DashboardPanel(t2, l2, width), DashboardPanel(t3, l3, width))
	}
	height := max(len(l1), max(len(l2), len(l3)))
	pad := func(l []string) []string {
		out := append([]string(nil), l...)
		for len(out) < height {
			out = append(out, "")
		}
		return out
	}
	const gap = 3
	avail := width - 2*gap
	w1 := avail / 3
	w2 := avail / 3
	w3 := avail - w1 - w2
	return lipgloss.JoinHorizontal(lipgloss.Top,
		DashboardPanel(t1, pad(l1), w1), strings.Repeat(" ", gap),
		DashboardPanel(t2, pad(l2), w2), strings.Repeat(" ", gap),
		DashboardPanel(t3, pad(l3), w3))
}

// AlignedEnds places left and right text at the ends of a width.
func AlignedEnds(left, right string, width int) string {
	right = FitLine(right, max(0, width/2))
	left = FitLine(left, max(0, width-lipgloss.Width(right)-2))
	return left + strings.Repeat(" ", max(2, width-lipgloss.Width(left)-lipgloss.Width(right))) + right
}

// Meter renders a filled proportion using block characters.
func Meter(fraction float64, width int) string {
	return usageMeter(fraction, width, Green)
}

// UsageMeter renders usage against a limit. Color shifts as the bar fills.
func UsageMeter(fraction float64, width int) string {
	style := Green
	if fraction >= 0.9 {
		style = Red
	} else if fraction >= 0.7 {
		style = Amber
	}
	return usageMeter(fraction, width, style)
}

// ColoredMeter renders a meter with an explicit fill style, for gauges where
// higher values are better (cache hit) or worse (resource usage).
func ColoredMeter(fraction float64, width int, fill lipgloss.Style) string {
	return usageMeter(fraction, width, fill)
}

func usageMeter(fraction float64, width int, fill lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	if fraction < 0 {
		fraction = 0
	}
	filled := min(width, max(0, int(fraction*float64(width)+0.5)))
	if fraction > 0 && filled == 0 {
		filled = 1
	}
	return fill.Render(strings.Repeat("█", filled)) + Muted.Render(strings.Repeat("░", width-filled))
}

// FitLine truncates a styled string to width without breaking escape sequences.
func FitLine(s string, width int) string {
	return ansi.Truncate(s, max(0, width), "…")
}

// WrappedText hard-wraps text at width columns.
func WrappedText(s string, width int) []string {
	return strings.Split(ansi.Hardwrap(s, max(1, width), true), "\n")
}
