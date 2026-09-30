package monitor

import (
	"strings"
	"time"
)

type RestoreProgress struct {
	Phase                            string  `json:"phase"`
	Identity                         string  `json:"identity"`
	WrittenBytes                     int64   `json:"written_bytes"`
	TotalBytes                       int64   `json:"total_bytes"`
	CPUSeconds                       float64 `json:"cpu_seconds"`
	SampleSeconds                    float64 `json:"sample_seconds"`
	BackupID                         string  `json:"backup_id"`
	WALFiles                         int     `json:"wal_files"`
	LastWAL                          string  `json:"last_wal"`
	StartWAL                         string  `json:"start_wal"`
	Error                            string  `json:"error"`
	RedoLSN                          string
	Job, Pod, Container              string
	At                               time.Time
	WriteRate, WALFileRate, CPUCores float64
	HasRate                          bool
	WriteHistory, WALHistory         []float64
}

func IsRecoveryJob(j JobResource) bool {
	switch j.Metadata.Labels["cnpg.io/jobRole"] {
	case "full-recovery", "recovery":
		return true
	default:
		return false
	}
}

func JobFinished(j JobResource) bool {
	return j.Status.Succeeded > 0 || j.Status.CompletionTime != nil
}

func SelectedRecoveryJob(Jobs []JobResource) (JobResource, bool) {
	var chosen JobResource
	found := false
	for _, j := range Jobs {
		if !IsRecoveryJob(j) {
			continue
		}
		if !found || RecoveryJobRank(j) > RecoveryJobRank(chosen) ||
			(RecoveryJobRank(j) == RecoveryJobRank(chosen) && j.Metadata.CreationTimestamp.After(chosen.Metadata.CreationTimestamp)) {
			chosen, found = j, true
		}
	}
	return chosen, found
}

func RecoveryJobRank(j JobResource) int {
	if !JobFinished(j) && j.Status.Active > 0 {
		return 3
	}
	if !JobFinished(j) {
		return 2
	}
	if j.Status.Succeeded == 0 && j.Status.Failed > 0 {
		return 1
	}
	return 0
}

func ClusterRecovering(s Snapshot) bool {
	if j, ok := SelectedRecoveryJob(s.Jobs); ok && !JobFinished(j) {
		return true
	}
	if s.Cluster.Status.CurrentPrimary != "" {
		return false
	}
	phase := strings.ToLower(s.Cluster.Status.Phase)
	return strings.Contains(phase, "setting up") || strings.Contains(phase, "recover")
}

func RecoveryPodName(s Snapshot, job JobResource) string {
	role := job.Metadata.Labels["cnpg.io/jobRole"]
	instance := job.Metadata.Labels["cnpg.io/instanceName"]
	for i := range s.Pods {
		p := &s.Pods[i]
		if p.Metadata.Labels["cnpg.io/jobRole"] != role {
			continue
		}
		if instance != "" && p.Metadata.Labels["cnpg.io/instanceName"] != instance {
			continue
		}
		return p.Metadata.Name
	}
	return ""
}

func MatchingRecoveryBackup(s Snapshot, backupID string) *BackupResource {
	if backupID == "" {
		return nil
	}
	for i := range s.Backups {
		if s.Backups[i].Status.BackupID == backupID || s.Backups[i].Status.BackupName == backupID {
			return &s.Backups[i]
		}
	}
	return nil
}

func collectRestoreProgress(cfg Config, s *Snapshot) {
	old := s.RestoreProgress
	s.RestoreProgress = RestoreProgress{}
	job, ok := SelectedRecoveryJob(s.Jobs)
	if !ok || JobFinished(job) {
		return
	}
	p := RestoreProgress{Job: job.Metadata.Name, Pod: RecoveryPodName(*s, job), Container: "plugin-barman-cloud", At: time.Now()}
	defer func() { s.RestoreProgress = p }()
	if p.Pod == "" {
		p.Error = "Recovery pod has not been reported yet"
		return
	}
	var sample RestoreProgress
	if err := GetJSON(cfg, &sample, "exec", p.Pod, "-c", p.Container, "--", "python3", "-c", restoreProbe); err != nil {
		p.Error = "Restore process metrics unavailable: check pod exec permission and Python availability"
		return
	}
	p.Phase, p.Identity, p.WrittenBytes, p.TotalBytes = sample.Phase, sample.Identity, sample.WrittenBytes, sample.TotalBytes
	p.CPUSeconds, p.SampleSeconds, p.BackupID = sample.CPUSeconds, sample.SampleSeconds, sample.BackupID
	p.WALFiles, p.LastWAL, p.StartWAL, p.Error = sample.WALFiles, sample.LastWAL, sample.StartWAL, sample.Error
	if p.TotalBytes == 0 {
		p.TotalBytes = old.TotalBytes
	}
	if p.BackupID == "" {
		p.BackupID = old.BackupID
	}
	if p.StartWAL == "" {
		p.StartWAL = old.StartWAL
	}
	if old.Job == p.Job && old.Pod == p.Pod && old.Identity == p.Identity && old.Identity != "" && p.Error == "" {
		dt := p.SampleSeconds - old.SampleSeconds
		if dt > 0 && p.WrittenBytes >= old.WrittenBytes {
			p.WriteRate = float64(p.WrittenBytes-old.WrittenBytes) / dt
			if p.CPUSeconds >= old.CPUSeconds {
				p.CPUCores = (p.CPUSeconds - old.CPUSeconds) / dt
			}
			p.HasRate = true
			p.WriteHistory = AppendHistory(old.WriteHistory, p.WriteRate, 18)
		}
	}
	if old.Job == p.Job && old.Pod == p.Pod && p.Phase == "wal" && old.Phase == "wal" {
		dt := p.SampleSeconds - old.SampleSeconds
		if dt > 0 && p.WALFiles >= old.WALFiles {
			p.WALFileRate = float64(p.WALFiles-old.WALFiles) / dt
			p.WALHistory = AppendHistory(old.WALHistory, p.WALFileRate, 18)
		}
	}
	if p.Phase == "wal" {
		if out, err := KubectlExec(cfg, 3*time.Second, "exec", p.Pod, "-c", "full-recovery", "--", "pg_controldata", "/var/lib/postgresql/data/pgdata"); err == nil {
			p.RedoLSN = ParseRedoLSN(out)
		}
	}
}

func ParseRedoLSN(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "Latest checkpoint's REDO location") {
			continue
		}
		if i := strings.LastIndex(line, ":"); i >= 0 {
			return strings.TrimSpace(line[i+1:])
		}
	}
	return ""
}

func JobStart(j JobResource) time.Time {
	if j.Status.StartTime != nil && !j.Status.StartTime.IsZero() {
		return *j.Status.StartTime
	}
	return j.Metadata.CreationTimestamp
}
