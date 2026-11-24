package backups

import (
	"fmt"
	"strings"
	"time"

	"pgmon/internal/monitor"
	"pgmon/internal/tui/views/shared"
)

func recoveryLines(ctx shared.Context) []string {
	if !monitor.ClusterRecovering(ctx.Data) {
		if j, ok := monitor.SelectedRecoveryJob(ctx.Data.Jobs); ok && j.Status.Succeeded == 0 && j.Status.Failed > 0 && ctx.Data.Cluster.Status.CurrentPrimary == "" {
			return []string{shared.Red.Render("! Recovery job failed"), j.Metadata.Name, shared.Muted.Render("Primary is not ready. Check the recovery job and cluster events.")}
		}
		return nil
	}
	lines := []string{shared.Amber.Render("◷ Cluster recovery in progress")}
	if phase := ctx.Data.Cluster.Status.Phase; phase != "" {
		lines = append(lines, "Cluster phase    "+phase)
	}
	if src := ctx.Data.Cluster.Spec.Bootstrap.Recovery.Source; src != "" {
		lines = append(lines, "Recovery source  "+src)
	}
	if p := ctx.Data.RestoreProgress; p.BackupID != "" {
		if b := monitor.MatchingRecoveryBackup(ctx.Data, p.BackupID); b != nil {
			lines = append(lines, "Source backup    "+shared.ValueOr(b.Status.BackupName, b.Metadata.Name))
		} else {
			lines = append(lines, "Source backup    "+p.BackupID)
		}
	}
	if target := shared.ValueOr(ctx.Data.Cluster.Status.CurrentPrimary, ctx.Data.Cluster.Status.TargetPrimary); target != "" {
		lines = append(lines, "Target primary   "+target)
	}
	if len(ctx.Data.Cluster.Status.InitializingPVC) > 0 {
		lines = append(lines, "Initializing PVC "+strings.Join(ctx.Data.Cluster.Status.InitializingPVC, ", "))
	}
	if j, ok := monitor.SelectedRecoveryJob(ctx.Data.Jobs); ok {
		lines = append(lines, "Recovery job     "+j.Metadata.Name)
		lines = append(lines, "Job status       "+recoveryJobStatus(j))
		lines = append(lines, "Elapsed          "+shared.Duration(time.Since(monitor.JobStart(j))))
		if pod := monitor.RecoveryPodName(ctx.Data, j); pod != "" {
			lines = append(lines, "Recovery pod     "+pod)
		}
	}
	return append(lines, shared.Muted.Render("PostgreSQL is not queryable until the primary exists."), shared.Muted.Render("Recovery is a Job, not a Backup resource."))
}

func clusterRecoveryLines(ctx shared.Context, width int) []string {
	rec := recoveryLines(ctx)
	if len(rec) == 0 || !monitor.ClusterRecovering(ctx.Data) {
		return rec
	}
	base := append([]string{shared.PanelTitle.Render("Base backup restore")}, baseRestoreLines(ctx)...)
	wal := append([]string{shared.PanelTitle.Render("WAL recovery")}, walRestoreLines(ctx)...)
	inner := shared.DashboardInnerWidth(width)
	if inner < 88 {
		rec = append(rec, "")
		rec = append(rec, base...)
		rec = append(rec, "")
		return append(rec, wal...)
	}
	right := append(append([]string{}, base...), "")
	right = append(right, wal...)
	return shared.JoinSideColumns(rec, right, inner)
}

func recoveryJobStatus(j monitor.JobResource) string {
	if j.Status.Active > 0 {
		return "running"
	}
	if j.Status.Failed > 0 && !monitor.JobFinished(j) {
		return "retrying"
	}
	if j.Status.Failed > 0 && j.Status.Succeeded == 0 {
		return "failed"
	}
	if monitor.JobFinished(j) {
		return "completed"
	}
	return "pending"
}

func restoreSampleAge(ctx shared.Context) bool {
	p := ctx.Data.RestoreProgress
	if p.At.IsZero() {
		return false
	}
	maxAge := 2 * ctx.KubeEvery
	if ctx.KubeBusy {
		maxAge = 3 * ctx.KubeEvery
	}
	return time.Since(p.At) <= maxAge
}

func baseRestoreLines(ctx shared.Context) []string {
	if !monitor.ClusterRecovering(ctx.Data) {
		return nil
	}
	p := ctx.Data.RestoreProgress
	if p.At.IsZero() && p.Error == "" {
		return []string{shared.Muted.Render("Waiting for first sample")}
	}
	if p.Error != "" && p.Phase != "base" && p.WrittenBytes == 0 {
		return []string{shared.Amber.Render(p.Error)}
	}
	if !restoreSampleAge(ctx) {
		return []string{shared.Amber.Render("Progress stale · last sample " + shared.Relative(p.At))}
	}
	done := p.Phase == "wal" || p.Phase == "done"
	lines := []string{restoreBar(p.WrittenBytes, p.TotalBytes, done, 22)}
	if done {
		if p.TotalBytes > 0 {
			lines = append(lines, shared.Green.Render("Base backup extracted"))
			lines = append(lines, shared.BytesHuman(p.TotalBytes)+" from backup_manifest")
		} else {
			lines = append(lines, shared.Green.Render("Base backup extracted"))
		}
		return lines
	}
	if p.TotalBytes > 0 {
		pct := min(float64(p.WrittenBytes)/float64(p.TotalBytes)*100, 99.9)
		lines = append(lines, fmt.Sprintf("~%.1f%%  %s / %s", pct, shared.BytesHuman(p.WrittenBytes), shared.BytesHuman(p.TotalBytes)))
	} else if p.WrittenBytes > 0 {
		lines = append(lines, shared.BytesHuman(p.WrittenBytes)+" extracted · manifest not available yet")
	} else {
		lines = append(lines, shared.Muted.Render("Waiting for tar extract"))
	}
	if p.HasRate {
		lines = append(lines, "Write throughput "+shared.Rate(p.WriteRate)+"  "+shared.Spark(p.WriteHistory))
		if p.WriteRate == 0 {
			lines = append(lines, shared.Amber.Render("No write progress this interval"))
		} else if p.TotalBytes > p.WrittenBytes {
			lines = append(lines, "Est. remaining   "+shared.Duration(time.Duration(float64(p.TotalBytes-p.WrittenBytes)/p.WriteRate)*time.Second))
		}
		if p.CPUCores > 0 {
			lines = append(lines, fmt.Sprintf("Extract CPU      %.2f cores", p.CPUCores))
		}
	} else {
		lines = append(lines, "Write throughput collecting second sample…")
	}
	return append(lines, shared.Muted.Render("Estimate: tar write_bytes / backup_manifest. Sample "+shared.Relative(p.At)))
}

func walRestoreLines(ctx shared.Context) []string {
	if !monitor.ClusterRecovering(ctx.Data) {
		return nil
	}
	p := ctx.Data.RestoreProgress
	if p.At.IsZero() && p.Error == "" {
		return []string{shared.Muted.Render("WAL recovery starts after the base backup")}
	}
	if !restoreSampleAge(ctx) {
		return []string{shared.Amber.Render("Progress stale · last sample " + shared.Relative(p.At))}
	}
	if p.Phase != "wal" {
		lines := []string{shared.Muted.Render(walMotionBar(0, 22)), shared.Muted.Render("Waiting for base backup extract")}
		if p.StartWAL != "" {
			lines = append(lines, "Start WAL        "+p.StartWAL)
		}
		return append(lines, shared.Muted.Render("WAL remaining unknown until archive ends."))
	}
	lines := []string{walMotionBar(p.WALFiles, 22), shared.Amber.Render("Applying WAL")}
	if p.LastWAL != "" {
		lines = append(lines, "Last WAL         "+p.LastWAL)
	}
	if p.StartWAL != "" {
		lines = append(lines, "Start WAL        "+p.StartWAL)
	}
	if p.RedoLSN != "" {
		lines = append(lines, "Redo LSN         "+p.RedoLSN)
	}
	lines = append(lines, fmt.Sprintf("WAL files on disk %d", p.WALFiles))
	if len(p.WALHistory) > 0 {
		lines = append(lines, fmt.Sprintf("Apply rate       %s  %.2f files/s", shared.Spark(p.WALHistory), p.WALFileRate))
	} else {
		lines = append(lines, "Apply rate       collecting second sample…")
	}
	return append(lines, shared.Muted.Render("WAL remaining unknown until archive ends."), shared.Muted.Render("Sample "+shared.Relative(p.At)))
}

func recoverySummaryLines(ctx shared.Context) []string {
	if !monitor.ClusterRecovering(ctx.Data) {
		return recoveryLines(ctx)
	}
	p := ctx.Data.RestoreProgress
	lines := []string{shared.Amber.Render("◷ Cluster recovery in progress")}
	if p.Phase == "wal" {
		lines = append(lines, restoreBar(p.WrittenBytes, p.TotalBytes, true, 16)+"  base done")
		last := shared.ValueOr(p.LastWAL, "applying WAL")
		lines = append(lines, walMotionBar(p.WALFiles, 16)+"  "+shared.Truncate(last, 24))
	} else if p.TotalBytes > 0 && restoreSampleAge(ctx) {
		pct := min(float64(p.WrittenBytes)/float64(p.TotalBytes)*100, 99.9)
		lines = append(lines, restoreBar(p.WrittenBytes, p.TotalBytes, false, 16)+fmt.Sprintf("  ~%.1f%% base", pct))
		lines = append(lines, shared.Muted.Render(walMotionBar(0, 16))+"  WAL after base")
	} else {
		lines = append(lines, shared.Muted.Render("Collecting restore progress"))
	}
	return append(lines, shared.Muted.Render("3 Backups for restore visualization"))
}

func restoreBar(written, total int64, done bool, width int) string {
	if done {
		return shared.Cyan.Render(strings.Repeat("█", width))
	}
	if total <= 0 {
		return shared.Muted.Render(strings.Repeat("░", width))
	}
	fraction := float64(written) / float64(total)
	if fraction > 0.999 {
		fraction = 0.999
	}
	if fraction < 0 {
		fraction = 0
	}
	filled := min(width-1, max(0, int(fraction*float64(width))))
	return shared.Cyan.Render(strings.Repeat("█", filled)) + shared.Muted.Render(strings.Repeat("░", width-filled))
}

func walMotionBar(files int, width int) string {
	bar := []rune(strings.Repeat("░", width))
	if files <= 0 {
		return shared.Muted.Render(string(bar))
	}
	pos := files % max(1, width-2)
	for i := 0; i < 3 && pos+i < width; i++ {
		bar[pos+i] = '█'
	}
	return shared.Cyan.Render(string(bar))
}
