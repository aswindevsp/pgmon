package pods

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

// Every box has the same height so pyramid links meet each node cleanly.
const NodeHeight = 6
const minPyramidBox = 34
const pyramidGap = 2

// Render draws the Pods tab: pod list, selected pod details, and storage.
func Render(ctx shared.Context, width int) string {
	sections := []string{
		shared.DashboardPanel("Pods · j/k select · p switchover", podListLines(ctx), width),
		shared.DashboardPanel("Selected pod", podDetails(ctx), width),
		shared.DashboardPanel("Switchover", shared.SwitchoverActionLines(ctx, width), width),
	}
	if ctx.SwitchoverConfirmTarget != "" {
		title, lines := shared.SwitchoverConfirmPanel(ctx)
		sections = append(sections, shared.DashboardPanel(title, lines, width))
	}
	sections = append(sections, shared.DashboardPanel("Provisioned storage", storageLines(ctx), width))
	return shared.StackDashboards(sections...)
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
	inner := max(16, width-4)
	role, color := "REPLICA", shared.Green
	if n.Primary && monitor.ClusterRecovering(ctx.Data) && ctx.Data.Cluster.Status.CurrentPrimary == "" {
		role, color = "RECOVERING", shared.Amber
	} else if n.Primary {
		role, color = "PRIMARY", shared.Cyan
	} else if ctx.Data.Cluster.Status.CurrentPrimary == "" {
		role, color = "ROLE UNKNOWN", shared.Muted
	}
	marker := ""
	border := lipgloss.Color("#344155")
	if selected {
		marker, border = "▸ ", lipgloss.Color("#55D6E8")
	}
	status, health := shared.PodHealth(n.Pod)
	title := color.Bold(true).Render(marker+role+"  ") + shared.Bright.Bold(true).Render(n.Name)
	lines := []string{shared.AlignedEnds(title, health.Render(status), inner), shared.AlignedEnds(shared.Muted.Render(shared.NodeName(n)), shared.InstanceSignal(ctx, n), inner)}
	lines = append(lines, shared.ResourceLines(ctx, n, inner)...)
	for i := range lines {
		lines[i] = shared.FitLine(lines[i], inner)
	}
	return cardFrame(lines, width, border, n.Primary)
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

// Topology renders the cluster as a pyramid: primary on top, replicas below,
// with WAL sync flow drawn on the links. Broken or stale senders use dashed
// lines so a gap in replication is visible.
func Topology(ctx shared.Context, width int) string {
	width = max(20, width)
	nodes := ctx.Nodes()
	selected := shared.SelectedIndex(nodes, ctx.SelectedPod)
	if len(nodes) == 1 {
		return strings.Join([]string{nodeBox(ctx, nodes[0], width, selected == 0), shared.Muted.Render("  No replica pods reported")}, "\n")
	}
	if canPyramid(width, len(nodes)-1) {
		return pyramidTopology(ctx, nodes, selected, width)
	}
	return stackedTopology(ctx, nodes, selected, width)
}

func canPyramid(width, replicas int) bool {
	return replicas > 0 && replicas*minPyramidBox+(replicas-1)*pyramidGap <= width
}

func pyramidTopology(ctx shared.Context, nodes []shared.Node, selected, width int) string {
	replicas := nodes[1:]
	boxW := max(minPyramidBox, (width-(len(replicas)-1)*pyramidGap)/len(replicas))
	rowW := len(replicas)*boxW + (len(replicas)-1)*pyramidGap
	rowLeft := max(0, (width-rowW)/2)
	xs := make([]int, len(replicas))
	flows := make([]walFlow, len(replicas))
	boxes := make([]string, 0, len(replicas)*2-1)
	for i, n := range replicas {
		if i > 0 {
			boxes = append(boxes, spacer(pyramidGap, NodeHeight))
		}
		boxes = append(boxes, nodeBox(ctx, n, boxW, selected == i+1))
		xs[i] = rowLeft + i*(boxW+pyramidGap) + boxW/2
		flows[i] = replicaFlow(ctx, n.Name)
	}
	return strings.Join([]string{
		nodeBox(ctx, nodes[0], width, selected == 0),
		strings.Join(pyramidLinks(width, width/2, xs, flows, ctx), "\n"),
		centerBlock(lipgloss.JoinHorizontal(lipgloss.Top, boxes...), width),
	}, "\n")
}

func stackedTopology(ctx shared.Context, nodes []shared.Node, selected, width int) string {
	rows := []string{nodeBox(ctx, nodes[0], width, selected == 0)}
	rows = append(rows, flowTrunk(ctx, width)...)
	for i, n := range nodes[1:] {
		last := i == len(nodes)-2
		if i > 0 {
			rows = append(rows, replicaFlow(ctx, n.Name).style.Render("  │"))
		}
		rows = append(rows, stackedDrop(ctx, n, width, last))
		rows = append(rows, nodeBox(ctx, n, width, selected == i+1))
	}
	return strings.Join(rows, "\n")
}

type walFlow struct {
	style  lipgloss.Style
	dashed bool
	broken bool
	label  string
	detail string
}

func replicaFlow(ctx shared.Context, name string) walFlow {
	if !ctx.SQLLive {
		return walFlow{style: shared.Muted, dashed: true, label: "unknown", detail: "replication stale"}
	}
	if r := ctx.Replication(name); r != nil {
		f := walFlow{
			style:  shared.Green,
			label:  shared.ValueOr(r.State, "unknown"),
			detail: "lag " + shared.ReplayLag(r),
		}
		if r.State != "streaming" {
			f.style, f.dashed = shared.Amber, true
			if r.State == "" || r.State == "stopped" {
				f.broken, f.label, f.detail = true, "BROKEN", shared.ValueOr(r.State, "sender stopped")
			}
		} else if r.LagBytes != nil && *r.LagBytes > 1024*1024 {
			f.style = shared.Amber
		}
		return f
	}
	for _, join := range monitor.ReplicaJoins(ctx.Data, ctx.SQLLive) {
		if join.Name == name {
			return walFlow{style: shared.Amber, dashed: true, label: "joining", detail: shared.ReplicaJoinCompact(join)}
		}
	}
	if p, ok := ctx.Data.ReplayProgress[name]; ok {
		return walFlow{style: shared.Amber, dashed: true, label: "catching up", detail: shared.WALReplayCompact(ctx, p)}
	}
	return walFlow{style: shared.Red, dashed: true, broken: true, label: "BROKEN", detail: "no sender"}
}

func (f walFlow) head() rune {
	if f.broken {
		return '✕'
	}
	return '▼'
}

func (f walFlow) vertical() rune {
	if f.dashed {
		return '┆'
	}
	return '│'
}

func flowTrunk(ctx shared.Context, width int) []string {
	style := shared.Cyan
	if !ctx.SQLLive {
		style = shared.Muted
	}
	label := "WAL sync"
	if ctx.SQLLive && ctx.Data.Sql.WAL != nil {
		label += "  " + shared.Rate(ctx.Data.WalRate)
	}
	return []string{style.Render(shared.FitLine("  │  "+label, width))}
}

func stackedDrop(ctx shared.Context, n shared.Node, width int, last bool) string {
	f := replicaFlow(ctx, n.Name)
	branch, dash := "├", "─"
	if last {
		branch = "╰"
	}
	if f.dashed {
		dash = "┄"
	}
	line := "  " + branch + dash + string(f.head()) + " " + n.Name + "  " + f.label + " · " + f.detail
	return f.style.Render(shared.FitLine(line, width))
}

func pyramidLinks(width, primaryX int, xs []int, flows []walFlow, ctx shared.Context) []string {
	stem := trunkStyle(ctx, flows)
	rows := make([][]ink, 4)
	for i := range rows {
		rows[i] = newInkRow(width)
	}
	setInk(rows[0], primaryX, '│', stem)
	if len(xs) == 1 {
		v := flows[0].vertical()
		setInk(rows[0], xs[0], v, flows[0].style)
		setInk(rows[1], xs[0], v, flows[0].style)
	} else {
		drawFork(rows[1], primaryX, xs, flows, stem)
	}
	for i, x := range xs {
		f := flows[i]
		setInk(rows[2], x, f.vertical(), f.style)
		if room := captionRoom(xs, i, width); room > 1 {
			writeRunes(rows[2], x+2, shared.Truncate(flowCaption(f), room), f.style)
		}
		setInk(rows[3], x, f.head(), f.style)
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = renderInk(row)
	}
	return out
}

func drawFork(row []ink, primaryX int, xs []int, flows []walFlow, stem lipgloss.Style) {
	left, right := xs[0], xs[len(xs)-1]
	for x := left; x <= right && x < len(row); x++ {
		ch, style := '─', stem
		if dashedToward(x, primaryX, xs, flows) {
			ch = '┄'
			style = flows[nearestIndex(xs, x)].style
		}
		switch {
		case x == left:
			ch = '╭'
		case x == right:
			ch = '╮'
		case x == primaryX && hasInt(xs, primaryX):
			ch, style = '┼', stem
		case x == primaryX:
			ch, style = '┴', stem
		case hasInt(xs, x):
			ch = '┬'
			style = flows[nearestIndex(xs, x)].style
		}
		setInk(row, x, ch, style)
	}
}

func dashedToward(x, primaryX int, xs []int, flows []walFlow) bool {
	if x == primaryX {
		return false
	}
	for i, rx := range xs {
		if x < primaryX && rx <= x && flows[i].dashed {
			return true
		}
		if x > primaryX && rx >= x && flows[i].dashed {
			return true
		}
	}
	return false
}

func captionRoom(xs []int, i, width int) int {
	right := width - xs[i] - 2
	if i+1 < len(xs) {
		right = xs[i+1] - xs[i] - 2
	}
	return max(0, min(14, right))
}

func flowCaption(f walFlow) string {
	if f.broken {
		return f.label
	}
	if f.detail != "" {
		return f.detail
	}
	return f.label
}

func cardFrame(lines []string, width int, border lipgloss.Color, joinBottom bool) string {
	inner := max(0, width-2)
	fg := lipgloss.NewStyle().Foreground(border)
	out := []string{fg.Render("╭" + strings.Repeat("─", inner) + "╮")}
	for _, line := range lines {
		out = append(out, fg.Render("│")+line+strings.Repeat(" ", max(0, inner-lipgloss.Width(line)))+fg.Render("│"))
	}
	if !joinBottom {
		return strings.Join(append(out, fg.Render("╰"+strings.Repeat("─", inner)+"╯")), "\n")
	}
	mid := inner / 2
	left := fg.Render("╰" + strings.Repeat("─", mid))
	right := fg.Render(strings.Repeat("─", max(0, inner-mid-1)) + "╯")
	return strings.Join(append(out, left+shared.Cyan.Render("┬")+right), "\n")
}

func trunkStyle(ctx shared.Context, flows []walFlow) lipgloss.Style {
	if !ctx.SQLLive {
		return shared.Muted
	}
	for _, f := range flows {
		if !f.broken {
			return shared.Cyan
		}
	}
	return shared.Amber
}

type ink struct {
	ch    rune
	style lipgloss.Style
	set   bool
}

func newInkRow(width int) []ink {
	return make([]ink, width)
}

func setInk(row []ink, x int, ch rune, style lipgloss.Style) {
	if x >= 0 && x < len(row) {
		row[x] = ink{ch: ch, style: style, set: true}
	}
}

func writeRunes(row []ink, x int, text string, style lipgloss.Style) {
	for i, r := range []rune(text) {
		setInk(row, x+i, r, style)
	}
}

func writeCentered(row []ink, cx int, text string, style lipgloss.Style) {
	runes := []rune(text)
	writeRunes(row, cx-len(runes)/2, text, style)
}

func renderInk(row []ink) string {
	var b strings.Builder
	for _, c := range row {
		if !c.set {
			b.WriteByte(' ')
			continue
		}
		b.WriteString(c.style.Render(string(c.ch)))
	}
	return b.String()
}

func centerBlock(block string, width int) string {
	lines := strings.Split(block, "\n")
	blockW := 0
	for _, line := range lines {
		blockW = max(blockW, lipgloss.Width(line))
	}
	pad := max(0, (width-blockW)/2)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, shared.FitLine(strings.Repeat(" ", pad)+line, width))
	}
	return strings.Join(out, "\n")
}

func spacer(w, h int) string {
	line := strings.Repeat(" ", w)
	return strings.Repeat(line+"\n", max(0, h-1)) + line
}

func hasInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func nearestIndex(xs []int, v int) int {
	best := 0
	for i, x := range xs {
		if abs(x-v) < abs(xs[best]-v) {
			best = i
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
		lines = append(lines, style.Render(health)+fmt.Sprintf(" · %d restarts · created %s · %s", shared.Restarts(n.Pod), shared.Relative(n.Pod.Metadata.CreationTimestamp), shared.NodeName(n)))
		lines = append(lines, shared.ResourceLines(ctx, n, 64)...)
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
		if p, ok := ctx.Data.ReplayProgress[n.Name]; ok {
			lines = append(lines, "WAL replay  "+shared.WALReplaySummary(ctx, p),
				shared.Muted.Render("Live position is the instance's control-file recovery position, read read-only; the primary cannot observe this phase."))
		} else {
			lines = append(lines, shared.Muted.Render("No current replication measurement for this pod."))
		}
	}
	lines = append(lines, nodeStorage(ctx, n))
	return lines
}
