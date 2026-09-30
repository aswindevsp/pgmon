package backups

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

// Render draws the Backups tab using only information reported by CNPG and
// PostgreSQL. In particular, a backup resource or an archiver counter is not
// proof that a point-in-time restore will succeed.
func Render(ctx shared.Context, width int) string {
	var sections []string
	if rec := clusterRecoveryLines(ctx, width); len(rec) > 0 {
		sections = append(sections, shared.DashboardPanel("Cluster recovery", rec, width))
	}
	var running, pending []monitor.BackupResource
	for _, b := range ctx.Data.Backups {
		switch {
		case monitor.ActiveBackup(b):
			running = append(running, b)
		case strings.EqualFold(b.Status.Phase, "pending"):
			pending = append(pending, b)
		}
	}
	sort.Slice(running, func(i, j int) bool { return monitor.BackupStart(running[i]).Before(monitor.BackupStart(running[j])) })
	sort.Slice(pending, func(i, j int) bool { return monitor.BackupStart(pending[i]).Before(monitor.BackupStart(pending[j])) })
	runLines := []string{}
	for _, b := range running {
		runLines = append(runLines, shared.Bright.Bold(true).Render(b.Metadata.Name)+shared.Muted.Render("  "+backupMethod(b)+" · "+shared.ValueOr(b.Status.Phase, "phase unknown")))
		runLines = append(runLines, backupProgressLines(ctx, b)...)
	}
	if len(running) == 0 {
		runLines = append(runLines, shared.Muted.Render("No running backup reported"))
	}
	for _, b := range pending {
		runLines = append(runLines, shared.Amber.Render("◷ queued  ")+b.Metadata.Name)
	}
	if ctx.Data.ResourceErr != "" {
		runLines = append(runLines, shared.Amber.Render("Kubernetes data may be stale"))
	}

	sections = append(sections, shared.DashboardPanel(fmt.Sprintf("Running %d, queued %d", len(running), len(pending)), runLines, width))
	sections = append(sections, shared.DashboardPanel("Point-in-time recovery window", recoveryWindowLines(ctx), width))
	sections = append(sections, shared.DashboardPanel("Backups", backupHistoryLines(ctx, width), width))
	sections = append(sections, shared.DashboardPair("WAL archiving", archiveDashboardLines(ctx), "Restore check", restoreCheckLines(), width))
	return shared.StackDashboards(sections...)
}

func backupMethod(b monitor.BackupResource) string {
	if b.Status.Method != "" {
		return b.Status.Method
	}
	return shared.ValueOr(b.Spec.Method, "method unknown")
}

func recoveryWindowLines(ctx shared.Context) []string {
	lines := []string{}
	if first := ctx.Data.Cluster.Status.FirstRecoverabilityPoint; first != "" {
		lines = append(lines, "First recoverability point  "+first)
	} else {
		lines = append(lines, "First recoverability point  "+shared.Muted.Render("not reported by CNPG"))
	}
	if ctx.SQLLive && ctx.Data.Sql.Archiver != nil {
		a := ctx.Data.Sql.Archiver
		if a.LastArchivedTime != nil {
			lines = append(lines, "Latest archived WAL         "+a.LastArchivedTime.Local().Format("Jan 02 15:04 MST"))
		} else {
			lines = append(lines, "Latest archived WAL         "+shared.Muted.Render("not reported"))
		}
	} else {
		lines = append(lines, "Latest archived WAL         "+shared.Muted.Render("unavailable"))
	}
	lines = append(lines, shared.Muted.Render("WAL continuity and restore coverage have not been verified."))
	return lines
}

func backupHistoryLines(ctx shared.Context, width int) []string {
	Backups := append([]monitor.BackupResource(nil), ctx.Data.Backups...)
	sort.Slice(Backups, func(i, j int) bool {
		return monitor.BackupStart(Backups[i]).After(monitor.BackupStart(Backups[j]))
	})
	wide := width >= 96
	lines := []string{}
	if len(Backups) == 0 {
		lines = append(lines, shared.Muted.Render("No Backup resources found"))
	} else if wide {
		lines = append(lines, shared.Muted.Render(fmt.Sprintf("%-32s  %-15s  %-16s  %-9s  %s", "name", "method", "started", "took", "status")))
	}
	for _, b := range Backups {
		started := "—"
		if !b.Status.StartedAt.IsZero() {
			started = b.Status.StartedAt.Local().Format("Jan 02 15:04")
		}
		took := "—"
		if !b.Status.StartedAt.IsZero() && !b.Status.StoppedAt.IsZero() && b.Status.StoppedAt.After(b.Status.StartedAt) {
			took = shared.Duration(b.Status.StoppedAt.Sub(b.Status.StartedAt))
		}
		phase := shared.ValueOr(b.Status.Phase, "unknown")
		switch strings.ToLower(phase) {
		case "completed":
			phase = shared.Green.Render("✓ " + phase)
		case "failed":
			phase = shared.Red.Render("! " + phase)
		case "pending", "started", "running", "finalizing":
			phase = shared.Amber.Render("◷ " + phase)
		}
		if wide {
			lines = append(lines, fmt.Sprintf("%-32s  %-15s  %-16s  %-9s  %s", shared.Truncate(b.Metadata.Name, 32), shared.Truncate(backupMethod(b), 15), started, took, phase))
		} else {
			// Place status first, where it remains visible even at 48 columns.
			lines = append(lines, phase+"  "+b.Metadata.Name)
			lines = append(lines, shared.Muted.Render("  "+backupMethod(b)+" · started "+started+" · took "+took))
		}
	}
	lines = append(lines, "", shared.PanelTitle.Render("Schedules"))
	if len(ctx.Data.Schedules) == 0 {
		lines = append(lines, shared.Muted.Render("No ScheduledBackup resources found"))
	}
	for _, s := range ctx.Data.Schedules {
		lines = append(lines, s.Metadata.Name+"  "+shared.ValueOr(s.Spec.Schedule, "schedule not reported"))
		last := "not yet reported"
		if s.Status.LastScheduleTime != nil {
			last = s.Status.LastScheduleTime.Local().Format("Jan 02 15:04 MST")
		}
		lines = append(lines, shared.Muted.Render("  Last scheduled  "+last))
	}
	return lines
}

func archiveDashboardLines(ctx shared.Context) []string {
	if !ctx.SQLLive || ctx.Data.Sql.Archiver == nil {
		return []string{shared.Amber.Render("Archiver statistics unavailable")}
	}
	a := ctx.Data.Sql.Archiver
	lines := []string{}
	if a.LastArchivedWAL != "" {
		lines = append(lines, "Last archived  "+a.LastArchivedWAL)
	} else {
		lines = append(lines, "Last archived  "+shared.Muted.Render("not reported"))
	}
	if a.LastArchivedTime != nil {
		lines = append(lines, "Archived at    "+a.LastArchivedTime.Local().Format("Jan 02 15:04 MST"))
	}
	lines = append(lines, fmt.Sprintf("Archived count %d total", a.ArchivedCount), fmt.Sprintf("Failed count   %d total", a.FailedCount))
	if a.FailedCount > 0 && a.LastFailedTime != nil {
		lines = append(lines, shared.Amber.Render("Last failure   "+shared.Relative(*a.LastFailedTime)))
	}
	for _, c := range ctx.Data.Cluster.Status.Conditions {
		if c.Type == "ContinuousArchiving" {
			lines = append(lines, "CNPG condition "+c.Status)
			break
		}
	}
	return lines
}

func restoreCheckLines() []string {
	return []string{
		shared.Muted.Render("No restore validation is collected."),
		shared.Muted.Render("Backup completion does not verify a restore."),
	}
}

func backupProgressLines(ctx shared.Context, b monitor.BackupResource) []string {
	p := ctx.Data.BackupProgress
	if !monitor.ActiveBackup(b) {
		return nil
	}
	lines := []string{"Elapsed          " + shared.Duration(time.Since(monitor.BackupStart(b))), "Backup pod       " + shared.ValueOr(b.Status.InstanceID.PodName, "not assigned")}
	if p.Backup != b.Metadata.Name || p.At.IsZero() {
		return append(lines, "Progress         waiting for sample")
	}
	if p.Error != "" {
		return append(lines, shared.Amber.Render(p.Error))
	}
	maxAge := 2 * ctx.KubeEvery
	if ctx.KubeBusy {
		maxAge = 3 * ctx.KubeEvery
	}
	if time.Since(p.At) > maxAge {
		return append(lines, shared.Amber.Render("Progress stale · last sample "+shared.Relative(p.At)))
	}
	lines = append(lines, "Process read     "+shared.BytesHuman(p.ReadBytes))
	if p.TotalBytes > 0 {
		fraction := float64(p.ReadBytes) / float64(p.TotalBytes)
		pct := min(fraction*100, 99.9)
		filled := min(int(fraction*20), 19)
		lines = append(lines, shared.Cyan.Render("["+strings.Repeat("█", filled)+strings.Repeat("░", 20-filled)+fmt.Sprintf("] ~%.1f%%", pct)), "Database size    "+shared.BytesHuman(p.TotalBytes))
	}
	if p.HasRate {
		lines = append(lines, "Read throughput  "+shared.Rate(p.ReadRate), fmt.Sprintf("Backup CPU       %.2f cores", p.CPUCores))
		if p.ReadRate == 0 {
			lines = append(lines, shared.Amber.Render("No read progress this interval · may be uploading or finalizing"))
		}
		if p.ReadRate > 0 && p.TotalBytes > p.ReadBytes {
			lines = append(lines, "Est. read time   "+shared.Duration(time.Duration(float64(p.TotalBytes-p.ReadBytes)/p.ReadRate)*time.Second))
		}
	} else {
		lines = append(lines, "Read throughput  collecting second sample…")
	}
	return append(lines, shared.Muted.Render("Estimate: process reads / database size; not upload completion."), shared.Muted.Render("Only CNPG phase completed confirms success. Sample "+shared.Relative(p.At)))
}
