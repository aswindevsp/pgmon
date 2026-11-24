package locks

import (
	"fmt"
	"strings"
	"time"

	"pgmon/internal/monitor"
	"pgmon/internal/tui/views/shared"
)

// Render draws the Locks tab: blocking relationships and sessions waiting on locks.
func Render(ctx shared.Context, width int) string {
	if !ctx.SQLLive {
		return shared.DashboardPanel("Locks", []string{shared.Amber.Render("Live PostgreSQL metrics unavailable")}, width)
	}
	lines := []string{shared.Muted.Render("PID      blocker PIDs        wait event                  query")}
	for _, s := range shared.FilteredSessions(ctx) {
		if len(s.BlockingPIDs) == 0 && s.WaitEventType != "Lock" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%-8d %-19v %-27s %s", s.PID, s.BlockingPIDs, s.WaitEventType+":"+s.WaitEvent, strings.Join(strings.Fields(s.Query), " ")))
	}
	if len(lines) == 1 {
		lines = append(lines, shared.Green.Render("No lock waits in this sample"))
	}
	body := shared.StackDashboards(shared.DashboardPanel("Blocking chain", BlockingLines(ctx), width), shared.DashboardPanel("Lock waits · pg_blocking_pids", lines, width))
	if ctx.Detail {
		details := []string{}
		for _, s := range shared.FilteredSessions(ctx) {
			if len(s.BlockingPIDs) > 0 || s.WaitEventType == "Lock" {
				details = append(details, fmt.Sprintf("pid %d · %s", s.PID, s.Application))
				details = append(details, shared.WrappedText(s.Query, width-6)...)
			}
		}
		if len(details) == 0 {
			details = append(details, "No waiting query selected")
		}
		body = shared.StackDashboards(shared.DashboardPanel("Waiting query text", details, width), body)
	}
	return body
}

// BlockingLines renders the blocking relationships, shared with the Queries tab.
func BlockingLines(ctx shared.Context) []string {
	byPID := map[int]monitor.SessionStatus{}
	for _, s := range ctx.Data.Sql.Sessions {
		byPID[s.PID] = s
	}
	lines := []string{}
	for _, s := range shared.FilteredSessions(ctx) {
		for _, pid := range s.BlockingPIDs {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			blocker, ok := byPID[pid]
			if ok {
				lines = append(lines, shared.Amber.Render(fmt.Sprintf("pid %d %s %s app=%s", pid, blocker.State, shared.Duration(time.Duration(blocker.XactSeconds)*time.Second), blocker.Application)), "  "+strings.Join(strings.Fields(blocker.Query), " "))
			} else {
				lines = append(lines, shared.Amber.Render(fmt.Sprintf("pid %d blocker details unavailable", pid)))
			}
			lines = append(lines, shared.Red.Render(fmt.Sprintf("  └─ pid %d waiting · query age %s", s.PID, shared.Duration(time.Duration(s.QuerySeconds)*time.Second))), "     "+strings.Join(strings.Fields(s.Query), " "))
		}
	}
	if len(lines) == 0 {
		lines = append(lines, shared.Green.Render("No blocking sessions in this sample"))
	}
	return lines
}
