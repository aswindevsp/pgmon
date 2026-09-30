package queries

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/locks"
	"postgres-monitor/internal/tui/views/shared"
)

func filteredQueries(ctx shared.Context) []monitor.QueryStat {
	rows := []monitor.QueryStat{}
	for _, q := range ctx.Data.Sql.TopQueries {
		if strings.Contains(strings.ToLower(q.Query), strings.ToLower(ctx.Filter)) {
			rows = append(rows, q)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if ctx.SortCalls {
			return rows[i].Calls > rows[j].Calls
		}
		return rows[i].TotalExecTime > rows[j].TotalExecTime
	})
	return rows
}

// Render draws the Queries tab: top statements, session activity, and detail.
func Render(ctx shared.Context, width int) string {
	if !ctx.SQLLive {
		return shared.DashboardPanel("Queries", []string{shared.Amber.Render("Live PostgreSQL metrics unavailable")}, width)
	}
	rows := filteredQueries(ctx)
	lines := []string{shared.Muted.Render(fmt.Sprintf("  %7s  %10s  %9s   %s", "share %", "calls", "mean ms", "query")), ""}
	if width < 85 {
		lines = []string{}
	}
	total := float64(0)
	for _, q := range ctx.Data.Sql.TopQueries {
		total += q.TotalExecTime
	}
	for i, q := range rows {
		marker := "  "
		if i == min(ctx.Row, len(rows)-1) {
			marker = "▸ "
		}
		pct := float64(0)
		if total > 0 {
			pct = q.TotalExecTime / total * 100
		}
		if width < 85 {
			lines = append(lines, marker+shared.Cyan.Render(fmt.Sprintf("%.1f%%", pct))+fmt.Sprintf("   %d calls   %.2f ms mean", q.Calls, q.MeanExecTime), "  "+strings.Join(strings.Fields(q.Query), " "), "")
		} else {
			lines = append(lines, fmt.Sprintf("%s%s  %10d  %9.2f   %s", marker, shared.Cyan.Render(fmt.Sprintf("%7.1f", pct)), q.Calls, q.MeanExecTime, strings.Join(strings.Fields(q.Query), " ")))
		}
	}
	if ctx.Data.Sql.TopQueriesError != "" {
		lines = append(lines, shared.Amber.Render(ctx.Data.Sql.TopQueriesError))
	}
	if len(rows) == 0 {
		lines = append(lines, shared.Muted.Render("No statement statistics available or no matching queries."), shared.Muted.Render("pg_stat_statements must already be available; the monitor does not enable it."))
	}
	order := "total execution time"
	if ctx.SortCalls {
		order = "calls"
	}
	lines = append(lines, "", shared.Muted.Render("Share of returned top statements · cumulative · sorted by "+order+" · s sort"))
	body := shared.StackDashboards(shared.DashboardPanel("Top statements by "+order, lines, width), shared.DashboardPair("Sessions", sessionSummary(ctx), "Blocking chain", locks.BlockingLines(ctx), width))
	sessions := shared.FilteredSessions(ctx)
	detail := []string{shared.Muted.Render(fmt.Sprintf("  %-7s  %-21s  %-8s  %-17s  %s", "PID", "state", "age", "application", "query")), ""}
	if width < 100 {
		detail = []string{}
	}
	for i, s := range sessions {
		marker := "  "
		if i == min(ctx.Row, len(sessions)-1) {
			marker = "▸ "
		}
		if width < 100 {
			detail = append(detail, fmt.Sprintf("%s%d   %s   %s", marker, s.PID, s.State, shared.Duration(time.Duration(s.QuerySeconds)*time.Second)), shared.Muted.Render("  "+shared.ValueOr(s.Application, "No application name")), "  "+strings.Join(strings.Fields(s.Query), " "), "")
		} else {
			detail = append(detail, fmt.Sprintf("%s%-7d  %-21s  %-8s  %-17s  %s", marker, s.PID, s.State, shared.Duration(time.Duration(s.QuerySeconds)*time.Second), shared.Truncate(s.Application, 17), strings.Join(strings.Fields(s.Query), " ")))
		}
	}
	if len(sessions) == 0 {
		detail = append(detail, shared.Muted.Render("No matching sessions"))
	}
	body = shared.StackDashboards(body, shared.DashboardPanel("Session activity · up to 200 · / filter", detail, width))
	if ctx.Detail {
		text := "No query selected"
		if len(rows) > 0 {
			text = rows[min(ctx.Row, len(rows)-1)].Query
		} else if len(sessions) > 0 {
			text = sessions[min(ctx.Row, len(sessions)-1)].Query
		}
		body = shared.StackDashboards(shared.DashboardPanel("Collected query text · up to 4096 characters · Enter closes", shared.WrappedText(text, width-6), width), body)
	}
	return body
}

func sessionSummary(ctx shared.Context) []string {
	lines := []string{}
	if a := ctx.Data.Sql.Activity; a != nil {
		lines = append(lines, fmt.Sprintf("Active        %s     Idle      %d", shared.Green.Render(fmt.Sprint(a.Active)), a.Idle), fmt.Sprintf("Idle in xact  %s     Blocked   %d", shared.Amber.Render(fmt.Sprint(a.IdleInXact)), a.Blocked), "")
	}
	waits := map[string]int{}
	for _, s := range ctx.Data.Sql.Sessions {
		if s.WaitEvent != "" {
			waits[s.WaitEventType+":"+s.WaitEvent]++
		} else if s.State == "active" {
			waits["CPU / running"]++
		}
	}
	names := []string{}
	largest := 1
	for n, c := range waits {
		names = append(names, n)
		largest = max(largest, c)
	}
	sort.Slice(names, func(i, j int) bool {
		if waits[names[i]] == waits[names[j]] {
			return names[i] < names[j]
		}
		return waits[names[i]] > waits[names[j]]
	})
	for _, n := range names {
		lines = append(lines, fmt.Sprintf("%-24s %s %d", n, shared.Meter(float64(waits[n])/float64(largest), 10), waits[n]))
	}
	if len(waits) == 0 {
		lines = append(lines, shared.Muted.Render("No wait events in collected sessions"))
	}
	return lines
}
