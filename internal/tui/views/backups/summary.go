package backups

import (
	"fmt"
	"strings"
	"time"

	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

// OverviewLines returns the Overview tab's backup status: one scannable row
// for a completed, running, or failed backup, or a short recovery block while
// the cluster is restoring. Detail stays on the Backups tab.
func OverviewLines(ctx shared.Context) []string {
	if monitor.ClusterRecovering(ctx.Data) {
		if rec := recoverySummaryLines(ctx); len(rec) > 0 {
			return rec
		}
	}
	if b, ok := monitor.SelectedBackup(ctx.Data.Backups); ok {
		return []string{overviewBackupLine(ctx, b)}
	}
	if len(ctx.Data.Schedules) > 0 {
		return []string{shared.Muted.Render("○ no backups yet") + "   cron " + ctx.Data.Schedules[0].Spec.Schedule}
	}
	return []string{shared.Muted.Render("No backup resources found")}
}

func overviewBackupLine(ctx shared.Context, b monitor.BackupResource) string {
	phase := shared.ValueOr(b.Status.Phase, "pending")
	status := shared.Green.Render("● ")
	switch {
	case strings.EqualFold(phase, "failed"):
		status = shared.Red.Render("! ")
	case !strings.EqualFold(phase, "completed"):
		status = shared.Amber.Render("◷ ")
	}
	parts := []string{status + phase, shared.Bright.Render(b.Metadata.Name)}
	if monitor.ActiveBackup(b) {
		parts = append(parts, shared.Duration(time.Since(monitor.BackupStart(b)))+" elapsed")
		if p := ctx.Data.BackupProgress; p.Backup == b.Metadata.Name && p.ReadBytes > 0 {
			parts = append(parts, shared.BytesHuman(p.ReadBytes)+" read")
		}
		if pod := b.Status.InstanceID.PodName; pod != "" {
			parts = append(parts, "on "+pod)
		}
	} else {
		when := b.Metadata.CreationTimestamp
		if !b.Status.StoppedAt.IsZero() {
			when = b.Status.StoppedAt
		}
		parts = append(parts, shared.Relative(when))
		if pod := b.Status.InstanceID.PodName; pod != "" {
			parts = append(parts, "on "+pod)
		}
		if b.Status.Error != "" {
			parts = append(parts, shared.Red.Render(shared.Truncate(b.Status.Error, 40)))
		}
	}
	if len(ctx.Data.Schedules) > 0 {
		parts = append(parts, "cron "+ctx.Data.Schedules[0].Spec.Schedule)
	}
	return strings.Join(parts, "   ")
}

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
