package pods

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

// Every box has the same height so the branches meet the center of each node.
const NodeHeight = 7

// Render draws the Pods tab: pod list, selected pod details, and storage.
func Render(ctx shared.Context, width int) string {
	return shared.StackDashboards(
		shared.DashboardPanel("Pods · j/k select", podListLines(ctx), width),
		shared.DashboardPanel("Selected pod", podDetails(ctx), width),
		shared.DashboardPanel("Provisioned storage", storageLines(ctx), width),
	)
}

func podListLines(ctx shared.Context) []string {
	nodes := ctx.Nodes()
	lines := []string{}
	for i, n := range nodes {
		prefix := "  "
		if i == shared.SelectedIndex(nodes, ctx.SelectedPod) {
			prefix = "▸ "
		}
		health, style := shared.PodHealth(n.Pod)
		role := "replica"
		if n.Primary && monitor.ClusterRecovering(ctx.Data) && ctx.Data.Cluster.Status.CurrentPrimary == "" {
			role = "recovering"
		} else if n.Primary {
			role = "primary"
		}
		lines = append(lines, prefix+n.Name+"  "+shared.PanelTitle.Render(role)+"  "+style.Render(health))
	}
	return lines
}

func storageLines(ctx shared.Context) []string {
	lines := []string{shared.Muted.Render("PVC capacity; filesystem usage is not collected")}
	for _, p := range ctx.Data.Pvcs {
		lines = append(lines, p.Metadata.Name+"  "+shared.ValueOr(p.Status.Capacity["storage"], "—")+"  "+p.Status.Phase)
	}
	return lines
}

func nodeBox(ctx shared.Context, n shared.Node, width int, selected bool) string {
	role, color := "REPLICA", shared.Green
	if n.Primary && monitor.ClusterRecovering(ctx.Data) && ctx.Data.Cluster.Status.CurrentPrimary == "" {
		role, color = "RECOVERING", shared.Amber
	} else if n.Primary {
		role, color = "PRIMARY", shared.Cyan
	} else if ctx.Data.Cluster.Status.CurrentPrimary == "" {
		role, color = "ROLE UNKNOWN", shared.Muted
	}
	marker := "  "
	border := lipgloss.Color("#344155")
	if selected {
		marker, border = "▸ ", lipgloss.Color("#55D6E8")
	}
	status, health := shared.PodHealth(n.Pod)
	lines := []string{color.Bold(true).Render(marker+role+"  ") + shared.Bright.Bold(true).Render(n.Name), health.Render(status)}
	if n.Primary {
		if ctx.SQLLive && ctx.Data.Sql.WAL != nil {
			lines = append(lines, shared.Cyan.Render("WAL "+shared.Rate(ctx.Data.WalRate))+shared.Muted.Render("  → replicas"))
		} else {
			lines = append(lines, shared.Muted.Render("WAL rate unavailable"))
		}
	} else if r := ctx.Replication(n.Name); r != nil {
		state := shared.Green.Render(r.State)
		if r.State != "streaming" {
			state = shared.Amber.Render(r.State)
		}
		lines = append(lines, state+shared.Muted.Render(" · "+r.SyncState+" · lag "+shared.ReplayLag(r)))
	} else if ctx.SQLLive {
		lines = append(lines, shared.Amber.Render("! No matching WAL sender"))
	} else {
		lines = append(lines, shared.Muted.Render("? Replication unavailable"))
	}
	if n.Pod != nil {
		lines = append(lines, shared.Muted.Render(fmt.Sprintf("%d restarts · %s", shared.Restarts(n.Pod), shared.ValueOr(n.Pod.Spec.NodeName, "node pending"))))
	} else {
		lines = append(lines, shared.Muted.Render("Waiting for Kubernetes pod data"))
	}
	lines = append(lines, nodeStorage(ctx, n))
	for i := range lines {
		lines[i] = shared.FitLine(lines[i], width-4)
	}
	return lipgloss.NewStyle().Width(width-2).Padding(0, 1).Border(shared.CardBorder).
		BorderForeground(border).Render(strings.Join(lines, "\n"))
}

func nodeStorage(ctx shared.Context, n shared.Node) string {
	var volumes []string
	for _, p := range ctx.Data.Pvcs {
		if p.Metadata.Name == n.Name || p.Metadata.Name == n.Name+"-wal" {
			label := "data"
			if p.Metadata.Name != n.Name {
				label = "WAL"
			}
			volumes = append(volumes, label+" "+shared.ValueOr(p.Status.Capacity["storage"], "—")+" "+p.Status.Phase)
		}
	}
	if len(volumes) == 0 {
		return shared.Muted.Render("Storage · capacity unavailable")
	}
	return shared.Muted.Render(strings.Join(volumes, " · "))
}

// Topology renders the graphical primary/replica topology used by tests and the
// compact topology panels.
func Topology(ctx shared.Context, width int) string {
	nodes := ctx.Nodes()
	selected := shared.SelectedIndex(nodes, ctx.SelectedPod)
	if width < 96 || len(nodes) == 1 {
		rows := []string{nodeBox(ctx, nodes[0], width, selected == 0)}
		for i, n := range nodes[1:] {
			branch := "├"
			if i == len(nodes)-2 {
				branch = "╰"
			}
			rows = append(rows, shared.Muted.Render("  │  WAL replication"))
			box := strings.Split(nodeBox(ctx, n, width-5, selected == i+1), "\n")
			lineStyle, dash := shared.Cyan, "─"
			r := ctx.Replication(n.Name)
			if r == nil || r.State != "streaming" {
				lineStyle, dash = shared.Amber, "┄"
			}
			if !ctx.SQLLive {
				lineStyle = shared.Muted
			}
			for j, line := range box {
				prefix := "  │  "
				if j == NodeHeight/2 {
					prefix = "  " + branch + dash + "→"
				} else if i == len(nodes)-2 && j > NodeHeight/2 {
					prefix = "     "
				}
				rows = append(rows, lineStyle.Render(prefix)+line)
			}
		}
		if len(nodes) == 1 {
			rows = append(rows, shared.Muted.Render("  No replica pods reported"))
		}
		return strings.Join(rows, "\n")
	}
	const linkWidth = 13
	leftWidth := (width - linkWidth) / 2
	rightWidth := width - linkWidth - leftWidth
	var right []string
	for i, n := range nodes[1:] {
		if i > 0 {
			right = append(right, strings.Repeat(" ", rightWidth))
		}
		right = append(right, strings.Split(nodeBox(ctx, n, rightWidth, selected == i+1), "\n")...)
	}
	primary := strings.Split(nodeBox(ctx, nodes[0], leftWidth, selected == 0), "\n")
	first, last := NodeHeight/2, len(right)-1-NodeHeight/2
	source := (first + last) / 2
	primaryStart := source - NodeHeight/2
	rows := make([]string, len(right))
	for y := range rows {
		left := strings.Repeat(" ", leftWidth)
		if y >= primaryStart && y < primaryStart+len(primary) {
			left = primary[y-primaryStart]
		}
		link := []rune(strings.Repeat(" ", linkWidth))
		if y >= first && y <= last {
			link[6] = '│'
		}
		target := y >= first && (y-first)%(NodeHeight+1) == 0
		if target {
			link[6] = '├'
			if y == first {
				link[6] = '╭'
			}
			if y == last {
				link[6] = '╰'
			}
			for x := 7; x < linkWidth-1; x++ {
				link[x] = '─'
			}
			link[linkWidth-1] = '→'
		}
		if y == source {
			for x := 0; x < 6; x++ {
				link[x] = '─'
			}
			link[6] = '┤'
			if target {
				link[6] = '┼'
				if first == last {
					link[6] = '─'
				}
			}
		}
		linkColor := shared.Cyan
		if !ctx.SQLLive {
			linkColor = shared.Muted
		}
		if target {
			r := ctx.Replication(nodes[1+(y-first)/(NodeHeight+1)].Name)
			if r == nil || r.State != "streaming" {
				linkColor = shared.Amber
				if !ctx.SQLLive {
					linkColor = shared.Muted
				}
				for x := 7; x < linkWidth-1; x++ {
					link[x] = '┄'
				}
			}
		}
		rows[y] = left + linkColor.Render(string(link)) + right[y]
	}
	return strings.Join(rows, "\n")
}

func podDetails(ctx shared.Context) []string {
	nodes := ctx.Nodes()
	n := nodes[shared.SelectedIndex(nodes, ctx.SelectedPod)]
	role := "Replica · replays WAL from the primary"
	if n.Primary {
		role = "Primary · accepts writes and sends WAL to replicas"
	}
	if monitor.ClusterRecovering(ctx.Data) && ctx.Data.Cluster.Status.CurrentPrimary == "" {
		role = "Recovery job · restoring the primary from backup"
	} else if ctx.Data.Cluster.Status.CurrentPrimary == "" {
		role = "Role unavailable · waiting for cluster status"
	}
	lines := []string{shared.Bright.Bold(true).Render(n.Name) + shared.Muted.Render("  /  "+role)}
	if n.Pod != nil {
		health, style := shared.PodHealth(n.Pod)
		lines = append(lines, style.Render(health)+fmt.Sprintf(" · %d restarts · created %s", shared.Restarts(n.Pod), shared.Relative(n.Pod.Metadata.CreationTimestamp)), "Node  "+shared.ValueOr(n.Pod.Spec.NodeName, "not assigned"))
	} else {
		lines = append(lines, shared.Muted.Render("Pod details unavailable"))
	}
	if r := ctx.Replication(n.Name); r != nil && !n.Primary {
		gap := "—"
		if r.SentLSN != "" && r.ReplayLSN != "" {
			gap = shared.LSNGap(r.SentLSN, r.ReplayLSN)
		}
		lines = append(lines, fmt.Sprintf("Replication  %s · %s · replay lag %s · backlog %s", r.State, r.SyncState, shared.ReplayLag(r), gap), "Sent "+shared.ValueOr(r.SentLSN, "—")+"  →  written "+shared.ValueOr(r.WriteLSN, "—")+"  →  flushed "+shared.ValueOr(r.FlushLSN, "—")+"  →  replayed "+shared.ValueOr(r.ReplayLSN, "—"))
		if r.ReplayLag == nil {
			lines = append(lines, shared.Muted.Render("Lag — means PostgreSQL did not report a measurement."))
		}
	} else if n.Primary && ctx.SQLLive {
		lines = append(lines, fmt.Sprintf("PostgreSQL %s · %d connected WAL senders", ctx.Data.Sql.Version, len(ctx.Data.Sql.Replication)))
	} else if !n.Primary {
		lines = append(lines, shared.Muted.Render("No current replication measurement for this pod."))
	}
	lines = append(lines, nodeStorage(ctx, n))
	return lines
}
