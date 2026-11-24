package monitor

import "time"

type ReplicationStatus struct {
	ApplicationName string  `json:"application_name"`
	State           string  `json:"state"`
	SyncState       string  `json:"sync_state"`
	SentLSN         string  `json:"sent_lsn"`
	WriteLSN        string  `json:"write_lsn"`
	FlushLSN        string  `json:"flush_lsn"`
	ReplayLSN       string  `json:"replay_lsn"`
	ReplayLag       *string `json:"replay_lag"`
	LagBytes        *int64  `json:"lag_bytes"`
}

// BaseBackupProgress is reported by the primary while a replica join job
// copies its initial database files with pg_basebackup.
type BaseBackupProgress struct {
	PID                 int    `json:"pid"`
	ApplicationName     string `json:"application_name"`
	Phase               string `json:"phase"`
	BackupTotal         int64  `json:"backup_total"`
	BackupStreamed      int64  `json:"backup_streamed"`
	TablespacesTotal    int    `json:"tablespaces_total"`
	TablespacesStreamed int    `json:"tablespaces_streamed"`
}

type SQLSnapshot struct {
	Version          string                  `json:"version"`
	MaxConnections   int                     `json:"max_connections"`
	CurrentWALLSN    string                  `json:"current_wal_lsn"`
	TimelineID       int                     `json:"timeline_id"`
	Replication      []ReplicationStatus     `json:"replication"`
	BaseBackups      []BaseBackupProgress    `json:"base_backups"`
	ReplicationSlots []ReplicationSlotStatus `json:"replication_slots"`
	Database         *DatabaseStats          `json:"database"`
	Sessions         []SessionStatus         `json:"sessions"`
	TopQueries       []QueryStat             `json:"top_queries"`
	TopQueriesError  string                  `json:"top_queries_error"`
	Archiver         *struct {
		ArchivedCount    int64      `json:"archived_count"`
		LastArchivedWAL  string     `json:"last_archived_wal"`
		LastArchivedTime *time.Time `json:"last_archived_time"`
		FailedCount      int64      `json:"failed_count"`
		LastFailedWAL    string     `json:"last_failed_wal"`
		LastFailedTime   *time.Time `json:"last_failed_time"`
	} `json:"archiver"`
	WAL *struct {
		WALBytes         int64   `json:"wal_bytes"`
		WALSync          int64   `json:"wal_sync"`
		WALSyncTime      float64 `json:"wal_sync_time"`
		TrackWalIOTiming string  `json:"track_wal_io_timing"`
		WalSyncMethod    string  `json:"wal_sync_method"`
	} `json:"wal"`
	Activity *struct {
		Connections    int     `json:"connections"`
		Active         int     `json:"active"`
		Idle           int     `json:"idle"`
		IdleInXact     int     `json:"idle_in_xact"`
		Blocked        int     `json:"blocked"`
		LongestSeconds float64 `json:"longest_seconds"`
	} `json:"activity"`
}

type Snapshot struct {
	KubeAt, SqlAt         time.Time
	Cluster               ClusterResource
	Pods                  []PodResource
	Pvcs                  []PVCResource
	Backups               []BackupResource
	BackupProgress        BackupProgress
	Jobs                  []JobResource
	RestoreProgress       RestoreProgress
	Schedules             []ScheduledBackupResource
	Events                []EventResource
	Sql                   SQLSnapshot
	ResourceErr, SqlErr   string
	SqlPrimary            string
	WalRate               float64
	WalHistory            []float64
	TxRate                float64
	TxHistory             []float64
	ReplicationLagHistory map[string][]float64
	ReplayProgress        map[string]WALReplayProgress
	BaseBackupRates       map[string]float64
	PodUsage              map[string]ResourceUsage
	NodeUsage             map[string]ResourceUsage
	PodUsageHistory       map[string]UsageHistory
	NodeUsageHistory      map[string]UsageHistory
	NodeCaps              map[string]ResourceCap
	MetricsErr            string
}

// WALReplayProgress tracks a starting replica's archive WAL replay, measured
// read-only from its control file and backup manifest before the instance
// accepts SQL connections.
type WALReplayProgress struct {
	Pod             string
	State           string    // pg_controldata cluster state, e.g. "in archive recovery"
	Timeline        int       // latest checkpoint TimeLineID
	Position        uint64    // latest restartpoint location, bytes
	Live            uint64    // control-file minimum recovery ending location, bytes; 0 when unknown
	LiveAt          time.Time // when the live cursor was sampled
	SegmentSize     uint64    // bytes per WAL segment from the control file; 0 when unparsed
	Start           uint64    // backup_manifest Start-LSN, bytes; 0 when unknown
	Target          uint64    // backup_manifest End-LSN, bytes; 0 when unknown
	Restarts        int32
	At              time.Time // control-file sample time
	LastAdvance     time.Time // when the live cursor last moved
	RateBytesPerSec float64
	HasRate         bool
}

// UsageHistory is recent CPU and memory samples for one pod or node.
type UsageHistory struct {
	CPU    []float64
	Memory []float64
}

type DatabaseStats struct {
	Transactions int64   `json:"transactions"`
	Commits      int64   `json:"commits"`
	Rollbacks    int64   `json:"rollbacks"`
	BlksHit      int64   `json:"blks_hit"`
	BlksRead     int64   `json:"blks_read"`
	CacheHit     float64 `json:"cache_hit"`
	Deadlocks    int64   `json:"deadlocks"`
	TempFiles    int64   `json:"temp_files"`
	TempBytes    int64   `json:"temp_bytes"`
}

type SessionStatus struct {
	PID           int     `json:"pid"`
	Database      string  `json:"database"`
	User          string  `json:"user"`
	Application   string  `json:"application"`
	ClientAddr    string  `json:"client_addr"`
	State         string  `json:"state"`
	WaitEventType string  `json:"wait_event_type"`
	WaitEvent     string  `json:"wait_event"`
	Query         string  `json:"query"`
	QuerySeconds  float64 `json:"query_seconds"`
	XactSeconds   float64 `json:"xact_seconds"`
	BlockingPIDs  []int   `json:"blocking_pids"`
}

type ReplicationSlotStatus struct {
	SlotName          string `json:"slot_name"`
	SlotType          string `json:"slot_type"`
	Database          string `json:"database"`
	Active            bool   `json:"active"`
	RestartLSN        string `json:"restart_lsn"`
	ConfirmedFlushLSN string `json:"confirmed_flush_lsn"`
	RetainedBytes     *int64 `json:"retained_bytes"`
}

type QueryStat struct {
	Query         string  `json:"query"`
	Calls         int64   `json:"calls"`
	Rows          int64   `json:"rows"`
	TotalExecTime float64 `json:"total_exec_time"`
	MeanExecTime  float64 `json:"mean_exec_time"`
}
