package monitor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMonitoringQueriesAreReadOnlyAndBounded(t *testing.T) {
	for name, query := range map[string]string{
		"core":        monitoringQuery,
		"timeline":    timelineQuery,
		"top queries": topQueriesQuery,
	} {
		upper := strings.ToUpper(query)
		if !strings.HasPrefix(upper, "BEGIN READ ONLY;") || !strings.Contains(upper, "SET LOCAL STATEMENT_TIMEOUT") || !strings.HasSuffix(strings.TrimSpace(upper), "COMMIT;") {
			t.Errorf("%s query must use a timed read-only transaction", name)
		}
		for _, forbidden := range []string{"INSERT ", "UPDATE ", "DELETE ", "CREATE ", "ALTER ", "DROP ", "TRUNCATE ", "GRANT ", "REVOKE ", "SELECT PG_CANCEL_BACKEND", "SELECT PG_TERMINATE_BACKEND"} {
			if strings.Contains(upper, forbidden) {
				t.Errorf("%s query contains %q", name, forbidden)
			}
		}
	}
	args := ReadOnlySQLArgs(Config{Context: "ctx", Namespace: "db"}, "primary-1", monitoringQuery)
	if !reflect.DeepEqual(args[:8], []string{"--context", "ctx", "-n", "db", "exec", "primary-1", "-c", "postgres"}) {
		t.Fatalf("unexpected kubectl target: %v", args[:8])
	}
	if !strings.Contains(strings.Join(args, " "), "psql -X -A -t -q") || args[len(args)-2] != "-c" || args[len(args)-1] != monitoringQuery {
		t.Fatal("monitoring command must bypass psql configuration and pass a fixed query")
	}
}

func TestClusterStatusFailureDoesNotUseStalePrimary(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "kubectl.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$PGMON_TEST_LOG\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PGMON_TEST_LOG", logPath)
	previous := Snapshot{}
	previous.Cluster.Status.CurrentPrimary = "stale-primary"
	previous.Sql.Version = "previous sample"
	got, err := collect(Config{Context: "ctx", Namespace: "db", Cluster: "Cluster"}, previous, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.SqlErr == "" || got.Sql.Version != "previous sample" {
		t.Fatalf("stale sample must be marked unavailable: %+v", got)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), " exec ") || strings.Count(strings.TrimSpace(string(log)), "\n") != 0 {
		t.Fatalf("unexpected kubectl calls after failed cluster get: %q", log)
	}
}

func TestRecoveringClusterDoesNotQuerySQL(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "kubectl.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$PGMON_TEST_LOG\"\n" +
		"if printf '%s' \"$*\" | grep -q ' exec '; then exit 1; fi\n" +
		"printf '%s\\n' '{\"status\":{\"phase\":\"Setting up primary\",\"targetPrimary\":\"cluster-1\"}}'\n"
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PGMON_TEST_LOG", logPath)
	got, err := collect(Config{Context: "ctx", Namespace: "db", Cluster: "Cluster"}, Snapshot{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.SqlErr != "" || got.Cluster.Status.Phase != "Setting up primary" || !ClusterRecovering(got) {
		t.Fatalf("recovery should skip SQL without an error: %+v", got)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), " exec ") {
		t.Fatalf("SQL exec during recovery: %q", log)
	}
}

func TestDecodeMonitoringFixture(t *testing.T) {
	input := []byte(`{"version":"17.2","max_connections":100,"current_wal_lsn":"0/1234","replication":[{"application_name":"standby-1","lag_bytes":42}],"replication_slots":[{"slot_name":"slot-1","active":true,"retained_bytes":64}],"database":{"transactions":10,"commits":9,"rollbacks":1,"cache_hit":99.5},"activity":{"connections":4,"active":1,"idle":2,"idle_in_xact":1},"sessions":[{"pid":123,"blocking_pids":[456],"xact_seconds":12.5}]}`)
	var got SQLSnapshot
	if err := DecodeObject(input, &got); err != nil {
		t.Fatal(err)
	}
	if got.MaxConnections != 100 || got.CurrentWALLSN != "0/1234" || got.Database.Transactions != 10 || got.Activity.IdleInXact != 1 || got.Replication[0].LagBytes == nil || *got.Replication[0].LagBytes != 42 || got.ReplicationSlots[0].RetainedBytes == nil || *got.ReplicationSlots[0].RetainedBytes != 64 || got.Sessions[0].BlockingPIDs[0] != 456 {
		t.Fatalf("monitoring data did not decode: %+v", got)
	}
	if err := DecodeObject([]byte("BEGIN\nCOMMIT"), &got); err == nil {
		t.Fatal("expected an error for missing monitoring JSON")
	}
}

func TestAppendHistoryBoundsSamples(t *testing.T) {
	var history []float64
	for n := 0; n < 65; n++ {
		history = AppendHistory(history, float64(n), 60)
	}
	if len(history) != 60 || history[0] != 5 || history[59] != 64 {
		t.Fatalf("unexpected bounded history: %v", history)
	}
}

func TestHistoryUpdatesDoNotMutatePreviousSnapshot(t *testing.T) {
	backing := []float64{3, 99, 99}
	previous := map[string][]float64{"standby-1": backing[:1], "gone": {7}}
	lag := int64(5)
	next := UpdatedLagHistory(previous, []ReplicationStatus{{ApplicationName: "standby-1", LagBytes: &lag}})
	if !reflect.DeepEqual(next["standby-1"], []float64{3, 5}) || len(next) != 1 {
		t.Fatalf("unexpected new replication history: %+v", next)
	}
	if !reflect.DeepEqual(previous["standby-1"], []float64{3}) || backing[1] != 99 || !reflect.DeepEqual(previous["gone"], []float64{7}) {
		t.Fatalf("previous Snapshot changed: %+v, backing: %+v", previous, backing)
	}
	next["standby-1"][0] = 55
	if previous["standby-1"][0] != 3 {
		t.Fatal("new sample shares its history backing array with the previous Snapshot")
	}
}
