package replication

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

// Render presents only measurements from the current primary.
func Render(ctx shared.Context, width int) string {
	flow := replicationFlowLines(ctx, width)
	trend := replicationTrendLines(ctx)
	slots := replicationSlotLines(ctx)
	parts := []string{shared.DashboardPanel("WAL flow", flow, width)}
	if joins := monitor.ReplicaJoins(ctx.Data, ctx.SQLLive); len(joins) > 0 {
		parts = append(parts, shared.DashboardPanel("Replica initialization · physical base copy", replicaJoinLines(joins, width), width))
	}
	parts = append(parts, shared.DashboardPair("Replay backlog · recent samples", trend, "Replication slots", slots, width))
	return shared.StackDashboards(parts...)
}

func replicaJoinLines(joins []monitor.ReplicaJoin, width int) []string {
	lines := []string{}
	for _, join := range joins {
		lines = append(lines, shared.Bright.Bold(true).Render(join.Name)+"  "+shared.ReplicaJoinSummary(join))
		if join.Progress != nil {
			if fraction, ok := join.Progress.Fraction(); ok {
				lines = append(lines, "  "+shared.Meter(fraction, max(8, min(32, width-8))))
			}
		}
	}
	lines = append(lines, shared.Muted.Render("Copy speed is averaged between SQL samples; total bytes are estimated. The replica then starts and catches up on WAL."))
	return lines
}

func replicationFlowLines(ctx shared.Context, width int) []string {
	if !ctx.SQLLive {
		return []string{shared.Amber.Render("Replication measurements unavailable or stale")}
	}
	primary := shared.ValueOr(ctx.Data.Cluster.Status.CurrentPrimary, "unknown primary")
	current := ctx.Data.Sql.CurrentWALLSN
	intro := shared.Cyan.Render("PRIMARY ") + shared.Bright.Bold(true).Render(primary) +
		shared.Muted.Render("  current ") + shared.Cyan.Render(shared.ValueOr(current, "unavailable"))
	if ctx.Data.Sql.TimelineID > 0 {
		intro += shared.Muted.Render(fmt.Sprintf("  timeline %d", ctx.Data.Sql.TimelineID))
	}
	if ctx.Data.Sql.WAL != nil && len(ctx.Data.WalHistory) > 0 {
		intro += shared.Muted.Render("  writing ") + shared.Bright.Render(shared.Rate(ctx.Data.WalRate))
	}
	lines := []string{intro}
	replicas := append([]monitor.ReplicationStatus(nil), ctx.Data.Sql.Replication...)
	sort.Slice(replicas, func(i, j int) bool { return replicas[i].ApplicationName < replicas[j].ApplicationName })
	if len(replicas) == 0 {
		return append(lines, shared.Muted.Render("No WAL senders reported by the primary"))
	}
	maxBacklog := uint64(0)
	for _, r := range replicas {
		for _, lsn := range []string{r.SentLSN, r.WriteLSN, r.FlushLSN, r.ReplayLSN} {
			if gap, ok := walBacklog(current, lsn); ok && gap > maxBacklog {
				maxBacklog = gap
			}
		}
	}
	if current != "" {
		lines = append(lines, shared.Muted.Render("  Stage bars show bytes behind current WAL; scale "+shared.BytesHumanU(maxBacklog)))
	}
	for i, r := range replicas {
		lines = append(lines, "")
		branch := "├─→ "
		if i == len(replicas)-1 {
			branch = "╰─→ "
		}
		state := shared.Green.Render(shared.ValueOr(r.State, "unknown"))
		if r.State != "streaming" {
			state = shared.Amber.Render(shared.ValueOr(r.State, "unknown"))
		}
		lag := "unavailable"
		if r.LagBytes != nil && *r.LagBytes >= 0 {
			lag = shared.BytesHuman(*r.LagBytes)
		} else if gap, ok := walBacklog(current, r.ReplayLSN); ok {
			lag = shared.BytesHumanU(gap)
		}
		lines = append(lines, shared.Muted.Render(branch)+shared.Bright.Bold(true).Render(shared.ValueOr(r.ApplicationName, "unnamed replica"))+"  "+
			shared.Muted.Render(shared.ValueOr(r.SyncState, "sync unknown"))+"  "+state+shared.Muted.Render("  replay backlog ")+shared.Amber.Render(lag))
		for _, stage := range []struct{ label, lsn string }{
			{"sent", r.SentLSN}, {"write", r.WriteLSN}, {"flush", r.FlushLSN}, {"replay", r.ReplayLSN},
		} {
			gap, ok := walBacklog(current, stage.lsn)
			barSpace := max(0, min(24, width-53))
			line := fmt.Sprintf("     %-6s %-13s", stage.label, shared.ValueOr(stage.lsn, "unavailable"))
			if ok {
				if barSpace > 0 {
					line += " " + backlogBar(gap, maxBacklog, barSpace)
				}
				line += "  " + shared.BytesHumanU(gap) + " behind"
			} else {
				line += "  backlog unavailable"
			}
			lines = append(lines, line)
		}
	}
	return lines
}

func replicationTrendLines(ctx shared.Context) []string {
	if !ctx.SQLLive {
		return []string{shared.Muted.Render("Lag trend unavailable while SQL data is stale")}
	}
	if len(ctx.Data.Sql.Replication) == 0 {
		return []string{shared.Muted.Render("No replica lag to plot")}
	}
	var lines []string
	replicas := append([]monitor.ReplicationStatus(nil), ctx.Data.Sql.Replication...)
	sort.Slice(replicas, func(i, j int) bool { return replicas[i].ApplicationName < replicas[j].ApplicationName })
	for _, r := range replicas {
		history := ctx.Data.ReplicationLagHistory[r.ApplicationName]
		if len(history) < 2 {
			lines = append(lines, shared.Bright.Render(shared.Truncate(r.ApplicationName, 12))+"  "+shared.Muted.Render("collecting lag samples"))
			continue
		}
		if len(history) > 12 {
			history = history[len(history)-12:]
		}
		maxLag := float64(0)
		for _, v := range history {
			if v > maxLag {
				maxLag = v
			}
		}
		lines = append(lines, shared.Bright.Render(shared.Truncate(r.ApplicationName, 12))+"  "+lagSpark(history)+
			shared.Muted.Render("  max "+shared.BytesHuman(int64(maxLag))))
	}
	lines = append(lines, shared.Muted.Render(fmt.Sprintf("%d s per sample · backlog in bytes", int(ctx.RefreshEvery.Seconds()))))
	return lines
}

func replicationSlotLines(ctx shared.Context) []string {
	if !ctx.SQLLive {
		return []string{shared.Muted.Render("Slot measurements unavailable while SQL data is stale")}
	}
	if len(ctx.Data.Sql.ReplicationSlots) == 0 {
		return []string{shared.Muted.Render("No replication slots reported")}
	}
	slots := append([]monitor.ReplicationSlotStatus(nil), ctx.Data.Sql.ReplicationSlots...)
	sort.Slice(slots, func(i, j int) bool { return slots[i].SlotName < slots[j].SlotName })
	lines := []string{shared.Muted.Render("slot              type      active  retained")}
	for _, s := range slots {
		active := shared.Red.Render("no ")
		if s.Active {
			active = shared.Green.Render("yes")
		}
		retained := shared.Muted.Render("unavailable")
		if s.RetainedBytes != nil && *s.RetainedBytes >= 0 {
			retained = shared.Bright.Render(shared.BytesHuman(*s.RetainedBytes))
		}
		lines = append(lines, fmt.Sprintf("%-17s %-8s  %s  %s", shared.Truncate(s.SlotName, 17), shared.Truncate(s.SlotType, 8), active, retained))
	}
	return lines
}

func walBacklog(current, stage string) (uint64, bool) {
	a, okA := validWALLSN(current)
	b, okB := validWALLSN(stage)
	if !okA || !okB || b > a {
		return 0, false
	}
	return a - b, true
}

func validWALLSN(lsn string) (uint64, bool) {
	parts := strings.Split(lsn, "/")
	if len(parts) != 2 {
		return 0, false
	}
	hi, e1 := strconv.ParseUint(parts[0], 16, 32)
	lo, e2 := strconv.ParseUint(parts[1], 16, 32)
	return hi<<32 | lo, e1 == nil && e2 == nil
}

func backlogBar(gap, scale uint64, width int) string {
	if width <= 0 {
		return ""
	}
	if scale == 0 {
		return shared.Green.Render(strings.Repeat("·", width))
	}
	n := int(math.Round(float64(gap) / float64(scale) * float64(width)))
	if gap > 0 && n == 0 {
		n = 1
	}
	n = min(n, width)
	color := shared.Green
	if gap > 0 {
		color = shared.Amber
	}
	return color.Render(strings.Repeat("█", n)) + shared.Muted.Render(strings.Repeat("·", width-n))
}

func lagSpark(samples []float64) string {
	maxV := float64(0)
	for _, v := range samples {
		if v > maxV {
			maxV = v
		}
	}
	if maxV == 0 {
		return shared.Green.Render(strings.Repeat("▁", min(len(samples), 24)))
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, v := range samples {
		idx := int(math.Round(math.Max(0, v) / maxV * 7))
		b.WriteRune(blocks[min(idx, 7)])
	}
	return shared.Amber.Render(b.String())
}
