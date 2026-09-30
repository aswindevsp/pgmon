package monitor

import (
	"math"
	"sort"
	"strings"
	"time"
)

// ReplicaJoin pairs an active CNPG join job with pg_basebackup progress from
// the primary. The byte total is PostgreSQL's estimate, not a fixed target.
type ReplicaJoin struct {
	Name            string
	Progress        *BaseBackupProgress
	RateBytesPerSec *float64
}

func (p BaseBackupProgress) Fraction() (float64, bool) {
	if p.BackupTotal <= 0 {
		return 0, false
	}
	return math.Min(1, math.Max(0, float64(p.BackupStreamed)/float64(p.BackupTotal))), true
}

// CalculateBaseBackupRates measures bytes copied between two successful SQL
// samples. A new pg_basebackup process must collect a second sample first.
func CalculateBaseBackupRates(previous, current []BaseBackupProgress, elapsed time.Duration) map[string]float64 {
	if elapsed <= 0 {
		return nil
	}
	rates := map[string]float64{}
	for _, now := range current {
		if now.PID == 0 || now.Phase != "streaming database files" {
			continue
		}
		for _, before := range previous {
			if before.PID == now.PID && before.ApplicationName == now.ApplicationName && now.BackupStreamed >= before.BackupStreamed {
				rates[now.ApplicationName] = float64(now.BackupStreamed-before.BackupStreamed) / elapsed.Seconds()
				break
			}
		}
	}
	return rates
}

func ReplicaJoins(s Snapshot, sqlLive bool) []ReplicaJoin {
	joins := map[string]ReplicaJoin{}
	for _, job := range s.Jobs {
		if job.Metadata.Labels["cnpg.io/jobRole"] != "join" || job.Status.Active == 0 {
			continue
		}
		name := job.Metadata.Labels["cnpg.io/instanceName"]
		if name != "" {
			joins[name] = ReplicaJoin{Name: name}
		}
	}
	if sqlLive {
		for i := range s.Sql.BaseBackups {
			p := &s.Sql.BaseBackups[i]
			name := strings.TrimSuffix(p.ApplicationName, "-join")
			if name == p.ApplicationName || !strings.HasPrefix(name, s.Cluster.Metadata.Name+"-") {
				continue
			}
			join := joins[name]
			join.Name, join.Progress = name, p
			if rate, ok := s.BaseBackupRates[p.ApplicationName]; ok {
				join.RateBytesPerSec = &rate
			}
			joins[name] = join
		}
	}
	result := make([]ReplicaJoin, 0, len(joins))
	for _, join := range joins {
		result = append(result, join)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
