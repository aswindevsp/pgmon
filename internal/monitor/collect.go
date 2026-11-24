package monitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func collect(cfg Config, previous Snapshot, includeKube bool) (Snapshot, error) {
	s := previous
	if includeKube {
		CollectKube(cfg, &s)
	}
	CollectClusterSQL(cfg, &s)
	if !includeKube && s.SqlErr != "" && strings.HasPrefix(s.SqlErr, "cluster status unavailable:") {
		AddErr(&s.ResourceErr, errors.New(s.SqlErr))
	}
	return s, nil
}

func CollectKube(cfg Config, s *Snapshot) {
	s.ResourceErr = ""
	usageDone := make(chan Snapshot, 1)
	go func() {
		tmp := Snapshot{PodUsage: s.PodUsage, NodeUsage: s.NodeUsage, PodUsageHistory: s.PodUsageHistory, NodeUsageHistory: s.NodeUsageHistory}
		collectUsage(cfg, &tmp)
		usageDone <- tmp
	}()
	var Pods ResourceList
	var Nodes ResourceList
	var Pvcs ResourceList
	var Backups ResourceList
	var Jobs ResourceList
	var Schedules ResourceList
	var Events ResourceList
	tasks := []struct {
		name string
		out  any
		args []string
	}{
		{"pods", &Pods, []string{"get", "pods", "-l", "cnpg.io/cluster=" + cfg.Cluster, "-o", "json"}},
		{"nodes", &Nodes, []string{"get", "nodes", "-o", "json"}},
		{"PVCs", &Pvcs, []string{"get", "persistentvolumeclaims", "-l", "cnpg.io/cluster=" + cfg.Cluster, "-o", "json"}},
		{"backups", &Backups, []string{"get", "backups.postgresql.cnpg.io", "-o", "json"}},
		{"jobs", &Jobs, []string{"get", "jobs", "-l", "cnpg.io/cluster=" + cfg.Cluster, "-o", "json"}},
		{"backup schedules", &Schedules, []string{"get", "scheduledbackups.postgresql.cnpg.io", "-l", "cnpg.io/cluster=" + cfg.Cluster, "-o", "json"}},
		{"events", &Events, []string{"get", "events", "-o", "json"}},
	}
	type kubeResult struct {
		name string
		err  error
	}
	results := make(chan kubeResult, len(tasks))
	succeeded := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		task := task
		go func() { results <- kubeResult{name: task.name, err: GetJSON(cfg, task.out, task.args...)} }()
	}
	for range tasks {
		result := <-results
		if result.err != nil {
			AddErr(&s.ResourceErr, fmt.Errorf("%s: %w", result.name, result.err))
		} else {
			succeeded[result.name] = true
		}
	}
	if succeeded["pods"] {
		s.Pods = nil
		decodeItems(Pods.Items, &s.Pods)
	}
	if succeeded["nodes"] {
		var nodes []NodeResource
		decodeItems(Nodes.Items, &nodes)
		s.NodeCaps = nodeCapsByName(nodes)
	}
	if succeeded["PVCs"] {
		s.Pvcs = nil
		decodeItems(Pvcs.Items, &s.Pvcs)
	}
	if succeeded["backups"] {
		s.Backups = nil
		decodeItems(Backups.Items, &s.Backups)
		filtered := s.Backups[:0]
		for _, b := range s.Backups {
			if b.Spec.Cluster.Name == cfg.Cluster {
				filtered = append(filtered, b)
			}
		}
		s.Backups = filtered
		collectBackupProgress(cfg, s)
	}
	if succeeded["jobs"] {
		s.Jobs = nil
		decodeItems(Jobs.Items, &s.Jobs)
		collectRestoreProgress(cfg, s)
	}
	if succeeded["backup schedules"] {
		s.Schedules = nil
		decodeItems(Schedules.Items, &s.Schedules)
	}
	if succeeded["events"] {
		s.Events = nil
		decodeItems(Events.Items, &s.Events)
		FilterEvents(&s.Events, cfg.Cluster)
	}
	usage := <-usageDone
	if usage.PodUsage != nil {
		s.PodUsage = usage.PodUsage
		s.PodUsageHistory = usage.PodUsageHistory
	}
	if usage.NodeUsage != nil {
		s.NodeUsage = usage.NodeUsage
		s.NodeUsageHistory = usage.NodeUsageHistory
	}
	s.MetricsErr = usage.MetricsErr
	// Archive replay progress has its own five-second collector in the TUI
	// loop; it needs no data from this cycle.
	if len(succeeded) == len(tasks) {
		s.KubeAt = time.Now()
	}
}

func CollectClusterSQL(cfg Config, s *Snapshot) {
	// The primary can change during failover, so refresh cluster status for every SQL sample.
	var freshCluster ClusterResource
	clusterErr := GetJSON(cfg, &freshCluster, "get", "clusters.postgresql.cnpg.io", cfg.Cluster, "-o", "json")
	if clusterErr == nil {
		s.Cluster = freshCluster
	}
	previousSQLAt := s.SqlAt
	previousPrimary := s.SqlPrimary
	previousSQLErr := s.SqlErr
	previousBaseBackups := s.Sql.BaseBackups
	previousWAL := int64(0)
	if s.Sql.WAL != nil {
		previousWAL = s.Sql.WAL.WALBytes
	}
	previousTransactions := int64(0)
	if s.Sql.Database != nil {
		previousTransactions = s.Sql.Database.Transactions
	}
	var SqlErr error
	if clusterErr != nil {
		SqlErr = fmt.Errorf("cluster status unavailable: %w", clusterErr)
	} else if s.Cluster.Status.CurrentPrimary == "" && ClusterRecovering(*s) {
		s.SqlErr = ""
		return
	} else {
		SqlErr = CollectSQL(cfg, s)
	}
	if SqlErr != nil {
		s.SqlErr = SqlErr.Error()
		return
	}
	s.SqlErr = ""
	s.SqlAt = time.Now()
	s.SqlPrimary = s.Cluster.Status.CurrentPrimary
	s.BaseBackupRates = nil
	if previousPrimary == s.SqlPrimary && previousSQLErr == "" && !previousSQLAt.IsZero() {
		s.BaseBackupRates = CalculateBaseBackupRates(previousBaseBackups, s.Sql.BaseBackups, s.SqlAt.Sub(previousSQLAt))
	}
	if previousPrimary != s.SqlPrimary {
		s.WalRate = 0
		s.TxRate = 0
		s.WalHistory = nil
		s.TxHistory = nil
		s.ReplicationLagHistory = nil
	}
	if previousPrimary == s.SqlPrimary && !previousSQLAt.IsZero() {
		seconds := s.SqlAt.Sub(previousSQLAt).Seconds()
		if seconds > 0 && s.Sql.WAL != nil {
			if delta := s.Sql.WAL.WALBytes - previousWAL; delta >= 0 {
				s.WalRate = float64(delta) / seconds
				s.WalHistory = AppendHistory(s.WalHistory, s.WalRate, 18)
			} else {
				s.WalRate, s.WalHistory = 0, nil
			}
		}
		if seconds > 0 && s.Sql.Database != nil {
			if delta := s.Sql.Database.Transactions - previousTransactions; delta >= 0 {
				s.TxRate = float64(delta) / seconds
				s.TxHistory = AppendHistory(s.TxHistory, s.TxRate, 60)
			} else {
				s.TxRate, s.TxHistory = 0, nil
			}
		}
	}
	s.ReplicationLagHistory = UpdatedLagHistory(s.ReplicationLagHistory, s.Sql.Replication)
}

func decodeItems[T any](items []json.RawMessage, dst *[]T) {
	for _, raw := range items {
		var item T
		if json.Unmarshal(raw, &item) == nil {
			*dst = append(*dst, item)
		}
	}
}

func AddErr(dst *string, err error) {
	if *dst != "" {
		*dst += " · "
	}
	*dst += err.Error()
}

func CollectSQL(cfg Config, s *Snapshot) error {
	primary := s.Cluster.Status.CurrentPrimary
	if primary == "" {
		return errors.New("primary pod is unknown")
	}
	out, err := readOnlySQL(cfg, primary, monitoringQuery, 7*time.Second)
	if err != nil {
		return err
	}
	var result SQLSnapshot
	if err := DecodeObject(out, &result); err != nil {
		return err
	}
	CollectOptionalSQL(cfg, primary, &result)
	s.Sql = result
	return nil
}

func FilterEvents(Events *[]EventResource, Cluster string) {
	filtered := (*Events)[:0]
	for _, e := range *Events {
		if strings.HasPrefix(e.InvolvedObject.Name, Cluster) {
			filtered = append(filtered, e)
		}
	}
	*Events = filtered
	sort.Slice(*Events, func(i, j int) bool {
		return (*Events)[i].Metadata.CreationTimestamp.After((*Events)[j].Metadata.CreationTimestamp)
	})
	if len(*Events) > 4 {
		*Events = (*Events)[:4]
	}
}
