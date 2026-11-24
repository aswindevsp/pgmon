package monitor

import (
	"time"
)

type BackupProgress struct {
	Identity               string  `json:"identity"`
	ReadBytes              int64   `json:"read_bytes"`
	CPUSeconds             float64 `json:"cpu_seconds"`
	SampleSeconds          float64 `json:"sample_seconds"`
	Error                  string  `json:"error"`
	Backup, Pod, Container string
	At                     time.Time
	TotalBytes             int64
	ReadRate, CPUCores     float64
	HasRate                bool
}

func ActiveBackup(b BackupResource) bool {
	return b.Status.Phase == "started" || b.Status.Phase == "running" || b.Status.Phase == "finalizing"
}

func BackupStart(b BackupResource) time.Time {
	if !b.Status.StartedAt.IsZero() {
		return b.Status.StartedAt
	}
	if !b.Status.ReconciliationStartedAt.IsZero() {
		return b.Status.ReconciliationStartedAt
	}
	return b.Metadata.CreationTimestamp
}

// BackupProcessName returns the barman process --name to match.
// It returns status.backupName when reported. When empty, callers should
// discover the unique running process instead of guessing from
// reconciliationStartedAt, which can differ by a second from the real
// process name (e.g. recon 03:57:47 vs process backup-20261006035748).
func BackupProcessName(b BackupResource) string {
	return b.Status.BackupName
}

// Prefer the active backup over a newer pending scheduled backup.
func SelectedBackup(Backups []BackupResource) (BackupResource, bool) {
	var chosen BackupResource
	found := false
	for _, b := range Backups {
		if !found || (ActiveBackup(b) && !ActiveBackup(chosen)) ||
			(ActiveBackup(b) == ActiveBackup(chosen) && b.Metadata.CreationTimestamp.After(chosen.Metadata.CreationTimestamp)) {
			chosen, found = b, true
		}
	}
	return chosen, found
}

func collectBackupProgress(cfg Config, s *Snapshot) {
	b, ok := SelectedBackup(s.Backups)
	old := s.BackupProgress
	s.BackupProgress = BackupProgress{}
	if !ok || !ActiveBackup(b) {
		return
	}
	p := BackupProgress{Backup: b.Metadata.Name, Pod: b.Status.InstanceID.PodName, At: time.Now()}
	defer func() { s.BackupProgress = p }()
	if p.Pod == "" {
		p.Error = "Backup pod has not been reported yet"
		return
	}
	p.Container = "postgres"
	if b.Spec.Method == "plugin" || b.Status.Method == "plugin" {
		if b.Spec.PluginConfiguration.Name != "barman-cloud.cloudnative-pg.io" {
			p.Error = "Process progress is supported for Barman Cloud backups only"
			return
		}
		p.Container = "plugin-barman-cloud"
	}
	name := BackupProcessName(b)
	// Empty means status.backupName not reported yet: ask the probe to
	// discover the unique barman-cloud-backup process instead of guessing
	// from reconciliationStartedAt.
	var sample BackupProgress
	if err := GetJSON(cfg, &sample, "exec", p.Pod, "-c", p.Container, "--", "python3", "-c", backupProbe, name); err != nil {
		p.Error = "Process metrics unavailable: check pod exec permission and Python availability"
		return
	}
	p.Identity, p.ReadBytes, p.CPUSeconds, p.SampleSeconds, p.Error = sample.Identity, sample.ReadBytes, sample.CPUSeconds, sample.SampleSeconds, sample.Error
	if p.Error != "" {
		return
	}
	if old.Backup == p.Backup && old.Pod == p.Pod && old.Container == p.Container && old.Identity == p.Identity && old.Error == "" {
		dt := p.SampleSeconds - old.SampleSeconds
		if dt > 0 && p.ReadBytes >= old.ReadBytes && p.CPUSeconds >= old.CPUSeconds {
			p.ReadRate = float64(p.ReadBytes-old.ReadBytes) / dt
			p.CPUCores = (p.CPUSeconds - old.CPUSeconds) / dt
			p.HasRate = true
		}
		p.TotalBytes = old.TotalBytes
	}
	// Cache the size for this process; do not scan database sizes on every refresh.
	if p.TotalBytes == 0 {
		var size struct {
			Bytes int64 `json:"bytes"`
		}
		err := GetJSON(cfg, &size, "exec", p.Pod, "-c", "postgres", "--", "psql", "-X", "-q", "-A", "-t", "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", `SET statement_timeout='3s'; SET default_transaction_read_only=on; SELECT json_build_object('bytes',sum(pg_database_size(oid))) FROM pg_database;`)
		if err == nil {
			p.TotalBytes = size.Bytes
		}
	}
}
