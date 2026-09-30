package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// The fixed statements keep every database operation inside a read-only transaction.
// pg_stat_statements and pg_control_checkpoint are optional because installations and
// PostgreSQL roles differ; failures in either must not hide the core monitoring data.
const monitoringQuery = `BEGIN READ ONLY; SET LOCAL statement_timeout = '3s'; SELECT json_build_object(
 'version', current_setting('server_version'),
 'max_connections', current_setting('max_connections')::int,
 'current_wal_lsn', pg_current_wal_lsn()::text,
 'replication', (SELECT COALESCE(json_agg(r), '[]'::json) FROM (SELECT application_name,state,sync_state,sent_lsn,write_lsn,flush_lsn,replay_lsn,replay_lag,CASE WHEN replay_lsn IS NOT NULL THEN greatest(0,pg_wal_lsn_diff(pg_current_wal_lsn(),replay_lsn))::bigint END AS lag_bytes FROM pg_stat_replication) r),
 'base_backups', (SELECT COALESCE(json_agg(b), '[]'::json) FROM (SELECT p.pid,COALESCE(a.application_name,'') AS application_name,p.phase,COALESCE(p.backup_total,0) AS backup_total,COALESCE(p.backup_streamed,0) AS backup_streamed,COALESCE(p.tablespaces_total,0) AS tablespaces_total,COALESCE(p.tablespaces_streamed,0) AS tablespaces_streamed FROM pg_stat_progress_basebackup p LEFT JOIN pg_stat_activity a USING (pid)) b),
 'replication_slots', (SELECT COALESCE(json_agg(sl), '[]'::json) FROM (SELECT slot_name,slot_type,database,active,restart_lsn,confirmed_flush_lsn,CASE WHEN restart_lsn IS NOT NULL THEN greatest(0,pg_wal_lsn_diff(pg_current_wal_lsn(),restart_lsn))::bigint END AS retained_bytes FROM pg_replication_slots ORDER BY slot_name) sl),
 'archiver', (SELECT row_to_json(a) FROM pg_stat_archiver a),
 'wal', (SELECT row_to_json(w)::jsonb || jsonb_build_object('track_wal_io_timing',current_setting('track_wal_io_timing'),'wal_sync_method',current_setting('wal_sync_method')) FROM pg_stat_wal w),
 'database', (SELECT row_to_json(d) FROM (SELECT COALESCE(sum(xact_commit+xact_rollback),0)::bigint AS transactions,COALESCE(sum(xact_commit),0)::bigint AS commits,COALESCE(sum(xact_rollback),0)::bigint AS rollbacks,COALESCE(sum(blks_hit),0)::bigint AS blks_hit,COALESCE(sum(blks_read),0)::bigint AS blks_read,CASE WHEN COALESCE(sum(blks_hit+blks_read),0)>0 THEN round(100.0*sum(blks_hit)/sum(blks_hit+blks_read),2) ELSE 0 END AS cache_hit,COALESCE(sum(deadlocks),0)::bigint AS deadlocks,COALESCE(sum(temp_files),0)::bigint AS temp_files,COALESCE(sum(temp_bytes),0)::bigint AS temp_bytes FROM pg_stat_database WHERE datname IS NOT NULL) d),
 'activity', (SELECT row_to_json(x) FROM (SELECT count(*) FILTER (WHERE backend_type='client backend' AND pid <> pg_backend_pid())::int AS connections,count(*) FILTER (WHERE backend_type='client backend' AND state='active' AND pid <> pg_backend_pid())::int AS active,count(*) FILTER (WHERE backend_type='client backend' AND state='idle' AND pid <> pg_backend_pid())::int AS idle,count(*) FILTER (WHERE backend_type='client backend' AND state LIKE 'idle in transaction%' AND pid <> pg_backend_pid())::int AS idle_in_xact,count(*) FILTER (WHERE backend_type='client backend' AND cardinality(pg_blocking_pids(pid)) > 0)::int AS blocked,COALESCE(max(extract(epoch FROM (now()-query_start))) FILTER (WHERE backend_type='client backend' AND state='active' AND pid <> pg_backend_pid()),0)::float AS longest_seconds FROM pg_stat_activity) x),
 'sessions', (SELECT COALESCE(json_agg(a), '[]'::json) FROM (SELECT pid,COALESCE(datname,'') AS database,COALESCE(usename,'') AS "user",COALESCE(application_name,'') AS application,COALESCE(client_addr::text,'') AS client_addr,COALESCE(state,'') AS state,COALESCE(wait_event_type,'') AS wait_event_type,COALESCE(wait_event,'') AS wait_event,left(COALESCE(query,''),4096) AS query,COALESCE(extract(epoch FROM (now()-query_start)),0)::float AS query_seconds,COALESCE(extract(epoch FROM (now()-xact_start)),0)::float AS xact_seconds,pg_blocking_pids(pid) AS blocking_pids FROM pg_stat_activity WHERE backend_type='client backend' AND pid <> pg_backend_pid() ORDER BY xact_start ASC NULLS LAST,query_start ASC NULLS LAST,pid LIMIT 200) a)
)::text; COMMIT;`

const timelineQuery = `BEGIN READ ONLY; SET LOCAL statement_timeout = '1s'; SELECT json_build_object('timeline_id',(pg_control_checkpoint()).timeline_id)::text; COMMIT;`

const topQueriesQuery = `BEGIN READ ONLY; SET LOCAL statement_timeout = '1s'; SELECT COALESCE(json_agg(q),'[]'::json)::text FROM (SELECT left(query,4096) AS query,calls,rows,total_exec_time,mean_exec_time FROM pg_stat_statements WHERE query IS NOT NULL ORDER BY total_exec_time DESC LIMIT 15) q; COMMIT;`

func ReadOnlySQLArgs(cfg Config, primary, query string) []string {
	return []string{"--context", cfg.Context, "-n", cfg.Namespace, "exec", primary, "-c", "postgres", "--", "psql", "-X", "-A", "-t", "-q", "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", query}
}

func readOnlySQL(cfg Config, primary, query string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", ReadOnlySQLArgs(cfg, primary, query)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("read-only PostgreSQL query timed out")
		}
		return nil, fmt.Errorf("read-only PostgreSQL query: %s", strings.TrimSpace(stderr.String()))
	}
	return bytes.TrimSpace(out), nil
}

func DecodeObject(output []byte, dst any) error {
	start, end := bytes.IndexByte(output, '{'), bytes.LastIndexByte(output, '}')
	if start < 0 || end < start {
		return errors.New("PostgreSQL returned no monitoring row")
	}
	if err := json.Unmarshal(output[start:end+1], dst); err != nil {
		return fmt.Errorf("decode PostgreSQL monitoring JSON: %w", err)
	}
	return nil
}

func CollectOptionalSQL(cfg Config, primary string, result *SQLSnapshot) {
	var wg sync.WaitGroup
	var timelineID int
	var haveTimeline bool
	var queries []QueryStat
	var queryErr string
	wg.Add(2)
	go func() {
		defer wg.Done()
		out, err := readOnlySQL(cfg, primary, timelineQuery, 3*time.Second)
		if err != nil {
			return
		}
		var timeline struct {
			TimelineID int `json:"timeline_id"`
		}
		if DecodeObject(out, &timeline) == nil {
			timelineID = timeline.TimelineID
			haveTimeline = true
		}
	}()
	go func() {
		defer wg.Done()
		out, err := readOnlySQL(cfg, primary, topQueriesQuery, 3*time.Second)
		if err != nil {
			queryErr = err.Error()
			return
		}
		if err := json.Unmarshal(out, &queries); err != nil {
			queryErr = fmt.Sprintf("decode statement statistics: %v", err)
		}
	}()
	wg.Wait()
	if haveTimeline {
		result.TimelineID = timelineID
	}
	result.TopQueries = queries
	result.TopQueriesError = queryErr
}
