package overview

import (
	"fmt"
	"strings"
	"time"

	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/backups"
	"postgres-monitor/internal/tui/views/shared"
)

// Render draws the Overview tab: compact topology, load, backup summary,
// alerts, and recent Kubernetes events.
func Render(ctx shared.Context, width int) string {
	nodes := ctx.Nodes()
	top := []string{}
	for i, n := range nodes {
		health, style := shared.PodHealth(n.Pod)
		symbol := "●"
		if !strings.HasPrefix(health, "●") {
			symbol = "!"
		}
		if i == 0 {
			node := "—"
			if n.Pod != nil {
				node = n.Pod.Spec.NodeName
			}
			role := "PRIMARY"
			if monitor.ClusterRecovering(ctx.Data) && ctx.Data.Cluster.Status.CurrentPrimary == "" {
				role = "RECOVERING"
			} else if ctx.Data.Cluster.Status.CurrentPrimary == "" {
				role = "ROLE UNKNOWN"
			}
			top = append(top, style.Render(symbol)+" "+shared.Bright.Bold(true).Render(n.Name)+"  "+shared.PanelTitle.Render(role), shared.Muted.Render("  Node  ")+node)

			if ctx.SQLLive {
				timeline := "—"
				if ctx.Data.Sql.TimelineID > 0 {
					timeline = fmt.Sprint(ctx.Data.Sql.TimelineID)
				}
				top = append(top, "  WAL "+shared.Cyan.Render(shared.ValueOr(ctx.Data.Sql.CurrentWALLSN, "—"))+"  timeline "+timeline)
			}
		} else {
			branch := "├─"
			if i == len(nodes)-1 {
				branch = "└─"
			}
			s := branch + " " + style.Render(symbol) + " " + n.Name
			if r := ctx.Replication(n.Name); r != nil {
				s += "  " + r.SyncState + "  lag " + shared.ReplayLag(r)
				if r.LagBytes != nil {
					s += "  " + shared.BytesHuman(*r.LagBytes)
				}
			} else {
				s += shared.Muted.Render("  replication unavailable")
			}
			top = append(top, "", s)
		}
	}
	for _, join := range monitor.ReplicaJoins(ctx.Data, ctx.SQLLive) {
		top = append(top, "", shared.Amber.Render("◷ "+join.Name), "  "+shared.ReplicaJoinCompact(join))
	}
	load := []string{shared.Muted.Render("Waiting for PostgreSQL metrics")}
	if ctx.SQLLive && ctx.Data.Sql.Activity != nil {
		a := ctx.Data.Sql.Activity
		maxConn := ctx.Data.Sql.MaxConnections
		bar := ""
		if maxConn > 0 {
			bar = shared.Meter(float64(a.Connections)/float64(maxConn), 16)
		}
		tps := shared.Muted.Render("collecting second sample…")
		if len(ctx.Data.TxHistory) > 0 {
			tps = fmt.Sprintf("%s  %.1f/s", shared.Spark(ctx.Data.TxHistory[max(0, len(ctx.Data.TxHistory)-16):]), ctx.Data.TxRate)
		}
		load = []string{fmt.Sprintf("Connections %s  %d / %d", bar, a.Connections, maxConn), "TPS         " + tps}
		if d := ctx.Data.Sql.Database; d != nil {
			load = append(load, "", fmt.Sprintf("Cache hit    %.2f%%", d.CacheHit), fmt.Sprintf("Deadlocks    %d     Temp files  %d", d.Deadlocks, d.TempFiles), shared.Muted.Render("Counters since statistics reset"))
		}
		load = append(load, "", fmt.Sprintf("Active       %d     Idle        %d", a.Active, a.Idle), fmt.Sprintf("Idle in xact %d     Blocked     %d", a.IdleInXact, a.Blocked))
	}
	backupLines := backups.SummaryLines(ctx)
	if len(backupLines) > 7 {
		backupLines = backupLines[:7]
		backupLines = append(backupLines, shared.Muted.Render("3 Backups for progress and history"))
	}
	alerts := []string{clusterHealth(ctx)}
	if ctx.SQLLive {
		for _, r := range ctx.Data.Sql.Replication {
			if r.LagBytes != nil && *r.LagBytes > 1024*1024 {
				alerts = append(alerts, shared.Amber.Render("▲ "+r.ApplicationName+" replay backlog "+shared.BytesHuman(*r.LagBytes)))
			}
		}
		for _, s := range ctx.Data.Sql.Sessions {
			if s.State == "idle in transaction" && s.XactSeconds >= 60 {
				alerts = append(alerts, shared.Amber.Render(fmt.Sprintf("▲ pid %d idle in transaction %s", s.PID, shared.Duration(time.Duration(s.XactSeconds)*time.Second))))
			}
		}
		for _, s := range ctx.Data.Sql.ReplicationSlots {
			if !s.Active {
				alerts = append(alerts, shared.Amber.Render("▲ slot "+s.SlotName+" inactive"))
			}
		}
	}
	events := []string{}
	for _, e := range ctx.Data.Events {
		style := shared.Muted
		if e.Type == "Warning" {
			style = shared.Amber
		}
		events = append(events, shared.Muted.Render(e.Metadata.CreationTimestamp.Local().Format("15:04:05"))+"   "+style.Render(e.Type)+"   "+shared.Bright.Render(e.InvolvedObject.Name), "           "+e.Message, "")
	}
	if len(events) == 0 {
		events = append(events, shared.Muted.Render("No recent cluster events reported"))
	}
	return shared.StackDashboards(shared.DashboardPair("Topology", top, "Load", load, width), shared.DashboardPair("Backups and WAL archive", backupLines, "Alerts", alerts, width), shared.DashboardPanel("Events", events, width))
}

func clusterHealth(ctx shared.Context) string {
	if !ctx.Started {
		return shared.Muted.Render("◌ Discovering cluster")
	}
	if monitor.ClusterRecovering(ctx.Data) {
		phase := shared.ValueOr(ctx.Data.Cluster.Status.Phase, "recovering primary")
		return shared.Amber.Render("◷ " + phase)
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

// walLines and activityLines are the compact WAL and activity summaries that
// previously lived on the application model.
func walLines(ctx shared.Context) []string {
	lines := []string{}
	if ctx.Data.Sql.WAL == nil || ctx.Data.SqlErr != "" {
		return []string{shared.Amber.Render("! WAL statistics unavailable"), ""}
	}
	lines = append(lines, fmt.Sprintf("%s generated", shared.Rate(ctx.Data.WalRate)), shared.Spark(ctx.Data.WalHistory))
	if ctx.Data.Sql.WAL.TrackWalIOTiming != "on" {
		lines = append(lines, "Disk sync timing  "+shared.Muted.Render("Not enabled"))
	} else {
		lines = append(lines, fmt.Sprintf("Disk sync total   %.1f ms", ctx.Data.Sql.WAL.WALSyncTime))
	}
	if a := ctx.Data.Sql.Archiver; a != nil {
		if a.LastArchivedTime != nil {
			lines = append(lines, "Last archived     "+shared.Relative(*a.LastArchivedTime))
		} else {
			lines = append(lines, "Last archived     "+shared.Muted.Render("Not yet reported"))
		}
		lines = append(lines, fmt.Sprintf("Archive failures  %d total", a.FailedCount))
		if a.FailedCount > 0 && a.LastFailedTime != nil {
			lines = append(lines, shared.Muted.Render("Last failure "+shared.Relative(*a.LastFailedTime)))
		}
	} else {
		lines = append(lines, "Archive status    "+shared.Muted.Render("Unavailable"))
	}
	return lines
}

func activityLines(ctx shared.Context) []string {
	if ctx.Data.Sql.Activity == nil || ctx.Data.SqlErr != "" {
		return []string{shared.Amber.Render("! Activity unavailable"), ""}
	}
	a := ctx.Data.Sql.Activity
	return []string{fmt.Sprintf("Connections      %d", a.Connections), fmt.Sprintf("Active queries   %d", a.Active), fmt.Sprintf("Blocked queries  %d", a.Blocked), fmt.Sprintf("Longest active   %s", shared.Duration(time.Duration(a.LongestSeconds*float64(time.Second))))}
}
