package backups

import (
	"fmt"
	"strings"

	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

// SummaryLines returns the compact backup summary shown by the Overview tab.
func SummaryLines(ctx shared.Context) []string {
	if rec := recoverySummaryLines(ctx); len(rec) > 0 {
		return rec
	}
	if len(ctx.Data.Backups) == 0 {
		lines := []string{shared.Muted.Render("No Backup resources found")}
		if len(ctx.Data.Schedules) == 0 {
			lines = append(lines, "No schedule resources found")
		} else {
			lines = append(lines, "Schedule         "+ctx.Data.Schedules[0].Spec.Schedule)
			if last := ctx.Data.Schedules[0].Status.LastScheduleTime; last != nil {
				lines = append(lines, "Last scheduled   "+shared.Relative(*last))
			} else {
				lines = append(lines, "Last scheduled   "+shared.Muted.Render("not yet run"))
			}
		}
		return lines
	}
	b, _ := monitor.SelectedBackup(ctx.Data.Backups)
	status := shared.Green.Render("● ")
	if strings.EqualFold(b.Status.Phase, "failed") {
		status = shared.Red.Render("! ")
	} else if !strings.EqualFold(b.Status.Phase, "completed") {
		status = shared.Amber.Render("◷ ")
	}
	lines := []string{status + b.Status.Phase, "Last backup      " + shared.Relative(b.Metadata.CreationTimestamp), "Started          " + shared.Relative(monitor.BackupStart(b))}
	lines = append([]string{b.Metadata.Name}, lines...)
	lines = append(lines, backupProgressLines(ctx, b)...)
	pending := 0
	for _, x := range ctx.Data.Backups {
		if x.Status.Phase == "pending" {
			pending++
		}
	}
	if pending > 0 {
		lines = append(lines, fmt.Sprintf("Queued backups   %d", pending))
	}
	if len(ctx.Data.Schedules) > 0 {
		lines = append(lines, "Schedule         "+ctx.Data.Schedules[0].Spec.Schedule)
	} else {
		lines = append(lines, shared.Muted.Render("No schedule resource found"))
	}
	if b.Status.Error != "" {
		lines = append(lines, shared.Red.Render(shared.Truncate(b.Status.Error, 34)))
	}
	return lines
}
