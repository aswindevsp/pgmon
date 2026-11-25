package overview

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"pgmon/internal/monitor"
	"pgmon/internal/tui/views/backups"
	"pgmon/internal/tui/views/shared"
)

// minCardWidth is the narrowest instance card that can still show a CPU and
// memory meter. Below it the instances fall back to a compact tree.
const minCardWidth = 34

// Render draws the Overview tab: a compact instance strip, a Load / WAL /
// Attention vitals row, and a single backup status line. Detail lives on the
// dedicated tabs.
func Render(ctx shared.Context, width int) string {
	sections := []string{
		instancesPanel(ctx, width),
		shared.DashboardTriple("Load", loadLines(ctx), "WAL / replica", walReplicaLines(ctx), "Attention", attentionLines(ctx), width),
	}
	if ctx.SwitchoverConfirmTarget != "" {
		title, lines := shared.SwitchoverConfirmPanel(ctx)
		sections = append([]string{shared.DashboardPanel(title, lines, width)}, sections...)
	} else if ctx.SwitchoverNotice != "" || ctx.SwitchoverBusy || monitor.SwitchoverInProgress(ctx.Data.Cluster) {
		sections = append([]string{shared.DashboardPanel("Switchover", shared.SwitchoverActionLines(ctx, width), width)}, sections...)
	}
	if len(ctx.Data.Backups) > 0 || len(ctx.Data.Schedules) > 0 || monitor.ClusterRecovering(ctx.Data) {
		sections = append(sections, shared.DashboardPanel("Backup", backups.OverviewLines(ctx), width))
	}
	return shared.StackDashboards(sections...)
}

// instancesPanel renders the primary and replicas as one row of compact cards,
// or a tree when cards cannot share the inner width.
func instancesPanel(ctx shared.Context, width int) string {
	nodes := ctx.Nodes()
	for _, join := range monitor.ReplicaJoins(ctx.Data, ctx.SQLLive) {
		if !hasNode(nodes, join.Name) {
			nodes = append(nodes, shared.Node{Name: join.Name})
		}
	}
	inner := shared.DashboardInnerWidth(width)
	title := instancesTitle(ctx, nodes)
	if len(nodes) == 0 {
		return shared.DashboardPanel(title, []string{shared.Muted.Render("No instances reported")}, width)
	}
	var content []string
	if inner/max(1, len(nodes)) >= minCardWidth {
		content = instanceCards(ctx, nodes, inner)
	} else {
		content = instanceTree(ctx, nodes, inner)
	}
	return shared.DashboardPanel(title, content, width)
}

func hasNode(nodes []shared.Node, name string) bool {
	for _, n := range nodes {
		if n.Name == name {
			return true
		}
	}
	return false
}

func instancesTitle(ctx shared.Context, nodes []shared.Node) string {
	desired := ctx.Data.Cluster.Spec.Instances
	ready := ctx.Data.Cluster.Status.ReadyInstances
	if desired == 0 {
		desired = len(nodes)
		if ready == 0 {
			ready = desired
		}
	}
	title := fmt.Sprintf("Instances  %d/%d ready", ready, desired)
	if ctx.SQLLive && ctx.Data.Sql.WAL != nil {
		title += "  ·  WAL " + shared.SparkRange(shared.TrimHistory(ctx.Data.WalHistory, 8)) + " " + shared.Cyan.Render(shared.Rate(ctx.Data.WalRate))
	} else if ctx.Started {
		title += "  ·  " + shared.Muted.Render("WAL collecting")
	}
	if ctx.SQLLive && ctx.Data.Sql.TimelineID > 0 {
		title += fmt.Sprintf("  ·  timeline %d", ctx.Data.Sql.TimelineID)
	}
	return title
}

func instanceCards(ctx shared.Context, nodes []shared.Node, inner int) []string {
	selected := shared.SelectedIndex(nodes, ctx.SelectedPod)
	widths := distribute(inner, len(nodes))
	cards := make([]string, len(nodes))
	for i, n := range nodes {
		border := lipgloss.Color("#344155")
		if i == selected {
			border = lipgloss.Color("#55D6E8")
		}
		cards[i] = cardBox(instanceCardLines(ctx, n, widths[i], i == selected), widths[i], border)
	}
	return strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, cards...), "\n")
}

// distribute splits total into count near-equal widths, giving remainder
// columns to the leading cards.
func distribute(total, count int) []int {
	out := make([]int, count)
	if count == 0 {
		return out
	}
	base, extra := total/count, total%count
	for i := range out {
		out[i] = base
		if i < extra {
			out[i]++
		}
	}
	return out
}

func instanceCardLines(ctx shared.Context, n shared.Node, width int, selected bool) []string {
	inner := max(16, width-4)
	role, color := instanceRole(ctx, n)
	marker := ""
	if selected {
		marker = "▸ "
	}
	health, healthStyle := compactHealth(ctx, n)
	title := color.Bold(true).Render(marker+role+"  ") + shared.Bright.Bold(true).Render(n.Name)
	lines := []string{
		titleWithHealth(title, healthStyle.Render(health), inner),
		instanceSignalLine(ctx, n, inner),
	}
	lines = append(lines, shared.ResourceLines(ctx, n, inner)...)
	if p, ok := ctx.Data.ReplayProgress[n.Name]; ok {
		if meter := shared.WALReplayMeterLine(ctx, p, inner); meter != "" {
			lines = append(lines, shared.Muted.Render("WAL ")+meter)
		}
	}
	for i := range lines {
		lines[i] = shared.FitLine(lines[i], inner)
	}
	return lines
}

// compactHealth trims the verbose PodHealth label to the instance-card form.
func compactHealth(ctx shared.Context, n shared.Node) (string, lipgloss.Style) {
	health, style := shared.PodHealth(n.Pod)
	switch {
	case strings.HasPrefix(health, "●"):
		return "● ready", style
	case strings.HasPrefix(health, "! Running"):
		return "! not ready", style
	default:
		return health, style
	}
}

// titleWithHealth right-aligns health only when both fit; a tight card keeps
// the full instance name and drops health rather than cutting the name.
func titleWithHealth(title, health string, width int) string {
	tw, hw := lipgloss.Width(title), lipgloss.Width(health)
	if tw+hw+2 <= width {
		return title + strings.Repeat(" ", width-tw-hw) + health
	}
	return shared.FitLine(title, width)
}

func instanceSignalLine(ctx shared.Context, n shared.Node, width int) string {
	signal := shared.InstanceSignal(ctx, n)
	if lipgloss.Width(signal) >= width {
		return shared.FitLine(signal, width)
	}
	node := shared.Muted.Render(shared.NodeName(n))
	if lipgloss.Width(node)+2+lipgloss.Width(signal) <= width {
		return shared.AlignedEnds(node, signal, width)
	}
	return shared.FitLine(signal, width)
}

// instanceTree is the narrow fallback: one line per instance, no ASCII fork.
func instanceTree(ctx shared.Context, nodes []shared.Node, inner int) []string {
	selected := shared.SelectedIndex(nodes, ctx.SelectedPod)
	lines := make([]string, 0, len(nodes))
	for i, n := range nodes {
		role, color := instanceRole(ctx, n)
		marker := "  "
		if i == selected {
			marker = "▸ "
		}
		prefix := "  "
		if i > 0 {
			prefix = "├─ "
			if i == len(nodes)-1 {
				prefix = "╰─ "
			}
		}
		health, healthStyle := compactHealth(ctx, n)
		head := prefix + marker + color.Bold(true).Render(role) + "  " + shared.Bright.Bold(true).Render(n.Name) + "  " + healthStyle.Render(health)
		res := strings.Join(shared.ResourceLines(ctx, n, max(24, inner)), "   ")
		lines = append(lines, shared.FitLine(head+"   "+shared.InstanceSignal(ctx, n)+"   "+res, inner))
	}
	return lines
}

func instanceRole(ctx shared.Context, n shared.Node) (string, lipgloss.Style) {
	switch {
	case n.Primary && monitor.ClusterRecovering(ctx.Data) && ctx.Data.Cluster.Status.CurrentPrimary == "":
		return "RECOVERING", shared.Amber
	case n.Primary:
		return "PRIMARY", shared.Cyan
	case ctx.Data.Cluster.Status.CurrentPrimary == "":
		return "ROLE UNKNOWN", shared.Muted
	default:
		return "REPLICA", shared.Green
	}
}

// cardBox draws a bordered card whose total width is exactly width.
func cardBox(lines []string, width int, color lipgloss.Color) string {
	inner := max(0, width-2)
	fg := lipgloss.NewStyle().Foreground(color)
	out := []string{fg.Render("╭" + strings.Repeat("─", inner) + "╮")}
	for _, line := range lines {
		out = append(out, fg.Render("│")+line+strings.Repeat(" ", max(0, inner-lipgloss.Width(line)))+fg.Render("│"))
	}
	out = append(out, fg.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return strings.Join(out, "\n")
}

func loadLines(ctx shared.Context) []string {
	if !ctx.SQLLive || ctx.Data.Sql.Activity == nil {
		return []string{shared.Muted.Render("Waiting for PostgreSQL metrics")}
	}
	a := ctx.Data.Sql.Activity
	maxConn := ctx.Data.Sql.MaxConnections
	connBar := shared.Muted.Render(strings.Repeat("░", 16))
	if maxConn > 0 {
		connBar = shared.UsageMeter(float64(a.Connections)/float64(maxConn), 16)
	}
	lines := []string{"Conn  " + connBar + "  " + shared.Bright.Render(fmt.Sprintf("%d / %d", a.Connections, maxConn))}
	tps := shared.Muted.Render("collecting second sample…")
	if len(ctx.Data.TxHistory) > 0 {
		tps = shared.SparkRange(shared.TrimHistory(ctx.Data.TxHistory, 16)) + "  " + shared.Bright.Render(fmt.Sprintf("%.1f/s", ctx.Data.TxRate))
	}
	lines = append(lines, "TPS   "+tps)
	if d := ctx.Data.Sql.Database; d != nil {
		lines = append(lines, "Cache "+shared.ColoredMeter(d.CacheHit/100, 16, cacheStyle(d.CacheHit))+"  "+shared.Bright.Render(fmt.Sprintf("%.1f%%", d.CacheHit)))
		lines = append(lines, shared.Muted.Render(fmt.Sprintf("Deadlocks %d · temp %d · %s", d.Deadlocks, d.TempFiles, shared.BytesHuman(d.TempBytes))))
	} else {
		lines = append(lines, shared.Muted.Render("Cache hit unavailable"))
	}
	lines = append(lines, fmt.Sprintf("Act %d · Idle %d · Ixact %d · Blk %d", a.Active, a.Idle, a.IdleInXact, a.Blocked))
	return lines
}

func cacheStyle(hit float64) lipgloss.Style {
	switch {
	case hit >= 99:
		return shared.Green
	case hit >= 90:
		return shared.Amber
	default:
		return shared.Red
	}
}

func walReplicaLines(ctx shared.Context) []string {
	if !ctx.SQLLive || ctx.Data.SqlErr != "" || ctx.Data.Sql.WAL == nil {
		return []string{shared.Amber.Render("WAL / replica measurements stale")}
	}
	lines := []string{}
	if a := ctx.Data.Sql.Archiver; a != nil {
		last := "—"
		if a.LastArchivedTime != nil {
			last = shared.Relative(*a.LastArchivedTime)
		}
		lines = append(lines, fmt.Sprintf("Archive   last %s · %d fail", last, a.FailedCount))
	} else {
		lines = append(lines, shared.Muted.Render("Archive   unavailable"))
	}
	active := 0
	for _, s := range ctx.Data.Sql.ReplicationSlots {
		if s.Active {
			active++
		}
	}
	lines = append(lines, fmt.Sprintf("Slots     %d active / %d", active, len(ctx.Data.Sql.ReplicationSlots)))
	lines = append(lines, "Max lag   "+worstLag(ctx))
	lines = append(lines, "LSN       "+shared.ValueOr(ctx.Data.Sql.CurrentWALLSN, "—"))
	longest := "—"
	if act := ctx.Data.Sql.Activity; act != nil && act.LongestSeconds > 0 {
		longest = secondsLabel(act.LongestSeconds)
	}
	lines = append(lines, "Longest   "+longest+" active")
	return lines
}

// secondsLabel keeps sub-minute durations precise (4.2s) instead of rounding.
func secondsLabel(s float64) string {
	if s < 1 {
		return fmt.Sprintf("%.0fms", s*1000)
	}
	if s < 60 {
		return fmt.Sprintf("%.1fs", s)
	}
	return shared.Duration(time.Duration(s * float64(time.Second)))
}

// worstLag reports the replica with the largest byte backlog and its replay lag.
func worstLag(ctx shared.Context) string {
	reps := ctx.Data.Sql.Replication
	if len(reps) == 0 {
		return "—"
	}
	chosen := &reps[0]
	for i := range reps {
		if reps[i].LagBytes != nil && (chosen.LagBytes == nil || *reps[i].LagBytes > *chosen.LagBytes) {
			chosen = &reps[i]
		}
	}
	bytes := "—"
	if chosen.LagBytes != nil {
		bytes = shared.BytesHuman(*chosen.LagBytes)
	}
	return shared.ReplayLag(chosen) + " · " + bytes
}

// attentionLines ranks exceptions, worst first, and shows nothing when the
// cluster is healthy. Healthy status lives in the Instances panel title.
func attentionLines(ctx shared.Context) []string {
	if !ctx.Started {
		return []string{shared.Muted.Render("◌ Discovering cluster")}
	}
	lines := []string{}
	if monitor.ClusterRecovering(ctx.Data) {
		lines = append(lines, shared.Amber.Render("◷ "+shared.ValueOr(ctx.Data.Cluster.Status.Phase, "recovering primary")))
	} else if monitor.SwitchoverInProgress(ctx.Data.Cluster) {
		lines = append(lines, shared.Amber.Render("◷ switchover "+ctx.Data.Cluster.Status.CurrentPrimary+" → "+ctx.Data.Cluster.Status.TargetPrimary))
	} else if j, ok := monitor.SelectedRecoveryJob(ctx.Data.Jobs); ok && j.Status.Succeeded == 0 && j.Status.Failed > 0 && ctx.Data.Cluster.Status.CurrentPrimary == "" {
		lines = append(lines, shared.Red.Render("! Recovery job failed"))
	}
	for _, n := range ctx.Nodes() {
		health, _ := shared.PodHealth(n.Pod)
		if !strings.HasPrefix(health, "●") {
			lines = append(lines, shared.Amber.Render("▲ "+n.Name+" "+health))
		}
	}
	if ctx.SQLLive {
		for _, r := range ctx.Data.Sql.Replication {
			switch {
			case r.State != "" && r.State != "streaming":
				lines = append(lines, shared.Amber.Render("▲ "+r.ApplicationName+" "+r.State))
			case r.LagBytes != nil && *r.LagBytes > 1024*1024:
				lines = append(lines, shared.Amber.Render("▲ "+r.ApplicationName+" replay backlog "+shared.BytesHuman(*r.LagBytes)))
			}
		}
		for _, s := range ctx.Data.Sql.ReplicationSlots {
			if !s.Active {
				lines = append(lines, shared.Amber.Render("▲ slot "+s.SlotName+" inactive"))
			}
		}
		for _, s := range ctx.Data.Sql.Sessions {
			if s.State == "idle in transaction" && s.XactSeconds >= 60 {
				lines = append(lines, shared.Amber.Render(fmt.Sprintf("▲ pid %d idle in xact %s", s.PID, shared.Duration(time.Duration(s.XactSeconds)*time.Second))))
			}
		}
		if a := ctx.Data.Sql.Activity; a != nil && a.Blocked > 0 {
			lines = append(lines, shared.Amber.Render(fmt.Sprintf("▲ %d blocked session(s)", a.Blocked)))
		}
		if a := ctx.Data.Sql.Archiver; a != nil && a.FailedCount > 0 {
			lines = append(lines, shared.Amber.Render(fmt.Sprintf("▲ archive failures %d", a.FailedCount)))
		}
	}
	if b, ok := monitor.SelectedBackup(ctx.Data.Backups); ok {
		switch {
		case strings.EqualFold(b.Status.Phase, "failed"):
			lines = append(lines, shared.Red.Render("▲ backup failed "+b.Metadata.Name))
		case monitor.ActiveBackup(b):
			lines = append(lines, shared.Amber.Render("▲ backup running "+shared.Duration(time.Since(monitor.BackupStart(b)))))
		}
	} else if len(ctx.Data.Schedules) == 0 && !monitor.ClusterRecovering(ctx.Data) {
		lines = append(lines, shared.Amber.Render("▲ no backups or schedules"))
	}
	for _, e := range ctx.Data.Events {
		if e.Type != "Warning" {
			continue
		}
		lines = append(lines, shared.Amber.Render("▲ "+e.Metadata.CreationTimestamp.Local().Format("15:04")+" "+e.InvolvedObject.Name+" "+shared.Truncate(e.Message, 30)))
	}
	if len(lines) == 0 {
		return []string{shared.Muted.Render("No issues")}
	}
	if len(lines) > 5 {
		lines = lines[:5]
	}
	return lines
}

func clusterHealth(ctx shared.Context) string {
	if !ctx.Started {
		return shared.Muted.Render("◌ Discovering cluster")
	}
	if monitor.ClusterRecovering(ctx.Data) {
		phase := shared.ValueOr(ctx.Data.Cluster.Status.Phase, "recovering primary")
		return shared.Amber.Render("◷ " + phase)
	}
	if monitor.SwitchoverInProgress(ctx.Data.Cluster) {
		return shared.Amber.Render("◷ switchover to " + ctx.Data.Cluster.Status.TargetPrimary)
	}
	if ctx.Data.ResourceErr != "" || !ctx.SQLLive {
		return shared.Amber.Render("! Partial or stale data")
	}
	desired, ready := ctx.Data.Cluster.Spec.Instances, ctx.Data.Cluster.Status.ReadyInstances
	if desired == 0 {
		return shared.Muted.Render("? Instance count unknown")
	}
	if ready < desired {
		return shared.Amber.Render(fmt.Sprintf("! %d / %d instances ready", ready, desired))
	}
	for _, n := range ctx.Nodes() {
		health, _ := shared.PodHealth(n.Pod)
		if !strings.HasPrefix(health, "●") {
			return shared.Amber.Render("! Pod needs attention")
		}
		if !n.Primary {
			r := ctx.Replication(n.Name)
			if r == nil || r.State != "streaming" {
				return shared.Amber.Render("! Replication needs attention")
			}
		}
	}
	for _, c := range ctx.Data.Cluster.Status.Conditions {
		if (c.Type == "Ready" || c.Type == "ContinuousArchiving") && c.Status != "True" {
			return shared.Amber.Render("! " + c.Type + " condition: " + c.Status)
		}
	}
	return shared.Green.Render("● All instances ready")
}
