package monitor

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseLSNBytes(t *testing.T) {
	got, ok := ParseLSNBytes("19CC/5BE0F898")
	want := uint64(0x19CC)<<32 | uint64(0x5BE0F898)
	if !ok || got != want {
		t.Fatalf("ParseLSNBytes = %d, %v; want %d, true", got, ok, want)
	}
	if _, ok := ParseLSNBytes("19cc/5be0f898"); !ok {
		t.Fatal("lowercase LSN should parse")
	}
	for _, input := range []string{"", "19CC", "19CC/5BE0F898/1", "ZZZZ/5BE0F898", "19CC/5BE0F89G"} {
		if _, ok := ParseLSNBytes(input); ok {
			t.Errorf("LSN %q should not parse", input)
		}
	}
}

func TestParseControlFileUsesMinimumRecoveryAsLiveCursor(t *testing.T) {
	control := []byte(`   Database cluster state:               in archive recovery
   Latest checkpoint location:           19CC/5BE0F898
   Latest checkpoint's REDO location:    19CC/3A592488
   Latest checkpoint's TimeLineID:       33
   Time of latest checkpoint:            Tue Nov 24 05:58:55 2026
   Minimum recovery ending location:     19E7/73FDA3B8
   Backup start location:                0/0
   Bytes per WAL segment:                16777216
`)
	state, position, live, timeline, segSize, ok := ParseControlFile(control)
	want, _ := ParseLSNBytes("19CC/5BE0F898")
	wantLive, _ := ParseLSNBytes("19E7/73FDA3B8")
	if !ok || state != "in archive recovery" || position != want || live != wantLive || timeline != 33 || segSize != 16<<20 {
		t.Fatalf("ParseControlFile = %q, %d, %d, %d, %d, %v", state, position, live, timeline, segSize, ok)
	}
	if _, _, _, _, _, ok := ParseControlFile([]byte("Database cluster state:               in production\nLatest checkpoint location:           19CC/5BE0F898\nLatest checkpoint's TimeLineID:       33\n")); !ok {
		t.Fatal("a production control file still parses; collection filters on state")
	}
	if _, _, _, _, _, ok := ParseControlFile([]byte("Database cluster state:               in archive recovery\n")); ok {
		t.Fatal("control file without a checkpoint and timeline must not parse")
	}
}

func TestParseBackupManifestWALRanges(t *testing.T) {
	manifest := []byte(`{
    "Backup-Version": 15,
    "System-Identifier": 753,
    "WAL-Ranges": [
    { "Timeline": 33, "Start-LSN": "193E/2E0C5D10", "End-LSN": "19FA/A405C478" }
    ]
}`)
	start, end, timeline, ok := ParseBackupManifestWALRanges(manifest)
	wantStart, _ := ParseLSNBytes("193E/2E0C5D10")
	wantEnd, _ := ParseLSNBytes("19FA/A405C478")
	if !ok || start != wantStart || end != wantEnd || timeline != 33 {
		t.Fatalf("manifest = %d, %d, %d, %v", start, end, timeline, ok)
	}
	// Two ranges: the furthest end is the consistency target.
	combined := string(manifest) + "\n" + `{ "Timeline": 33, "Start-LSN": "193E/00000000", "End-LSN": "19F0/00000000" }`
	start, end, _, ok = ParseBackupManifestWALRanges([]byte(combined))
	if !ok || start != wantStart || end != wantEnd {
		t.Fatalf("manifest with several ranges picked %d..%d, %v; want %d..%d", start, end, ok, wantStart, wantEnd)
	}
	if _, _, _, ok := ParseBackupManifestWALRanges([]byte(`{"Backup-Version": 15}`)); ok {
		t.Fatal("manifest without a WAL range must not parse")
	}
}

func replayTestPod(t *testing.T, raw string) PodResource {
	t.Helper()
	var p PodResource
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReplayCandidatesFiltersInstancesUnderArchiveReplay(t *testing.T) {
	var s Snapshot
	s.SqlPrimary = "orders-db-1"
	s.Cluster.Status.CurrentPrimary = "orders-db-1"
	s.SqlAt = time.Now()
	notReady := `"status":{"phase":"Running","containerStatuses":[{"name":"postgres","ready":false,"restartCount":2}]}`
	s.Pods = []PodResource{
		replayTestPod(t, `{"metadata":{"name":"orders-db-ready"},`+`"status":{"phase":"Running","containerStatuses":[{"name":"postgres","ready":true}]}}`),
		replayTestPod(t, `{"metadata":{"name":"orders-db-zreplay"},`+notReady+`}`),
		replayTestPod(t, `{"metadata":{"name":"orders-db-1"},`+notReady+`}`),
		replayTestPod(t, `{"metadata":{"name":"orders-db-job","ownerReferences":[{"kind":"Job"}]},`+notReady+`}`),
		replayTestPod(t, `{"metadata":{"name":"orders-db-pending"},"status":{"phase":"Pending"}}`),
		replayTestPod(t, `{"metadata":{"name":"orders-db-streaming"},`+notReady+`}`),
		replayTestPod(t, `{"metadata":{"name":"orders-db-joining"},`+notReady+`}`),
		replayTestPod(t, `{"metadata":{"name":"orders-db-areplay"},`+notReady+`}`),
	}
	s.Sql.Replication = []ReplicationStatus{{ApplicationName: "orders-db-streaming", State: "streaming"}}
	job := JobResource{}
	job.Metadata.Labels = map[string]string{"cnpg.io/jobRole": "join", "cnpg.io/instanceName": "orders-db-joining"}
	job.Status.Active = 1
	s.Jobs = []JobResource{job}

	candidates := ReplayCandidates(s)
	got := make([]string, 0, len(candidates))
	for _, p := range candidates {
		got = append(got, p.Metadata.Name)
	}
	want := []string{"orders-db-areplay", "orders-db-zreplay"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
}

func TestReplayRateFreshOnLiveCursorAdvance(t *testing.T) {
	now := time.Now()
	previous := WALReplayProgress{Pod: "orders-db-2", Position: 1000, Live: 1000, Restarts: 1, At: now, LastAdvance: now, HasRate: true, RateBytesPerSec: 500}
	// The restartpoint is unchanged; only the live flush cursor moved.
	current := WALReplayProgress{Pod: "orders-db-2", Position: 1000, Live: 5000, Restarts: 1, At: now.Add(4 * time.Second)}
	rate, advanced, carried := replayRate(previous, current)
	if rate != 1000 || !advanced || carried {
		t.Fatalf("fresh rate = %.0f, %v, %v; want 1000, true, false", rate, advanced, carried)
	}
	applied := applyReplayRate(previous, current)
	if !applied.HasRate || applied.RateBytesPerSec != 1000 || !applied.LastAdvance.Equal(current.At) {
		t.Fatalf("fresh sample = %+v", applied)
	}
}

func TestReplayRateCarriesAcrossRestartAndQuietCursor(t *testing.T) {
	now := time.Now()
	lastAdvance := now.Add(-30 * time.Second)
	previous := WALReplayProgress{Pod: "orders-db-2", Position: 1000, Live: 1000, Restarts: 1, At: now, LastAdvance: lastAdvance, HasRate: true, RateBytesPerSec: 800}
	current := WALReplayProgress{Pod: "orders-db-2", Position: 1000, Live: 1000, Restarts: 1, At: now.Add(15 * time.Second)}
	if rate, advanced, carried := replayRate(previous, current); rate != 800 || advanced || !carried {
		t.Fatalf("quiet cursor rate = %.0f, %v, %v; want 800, false, true", rate, advanced, carried)
	}
	applied := applyReplayRate(previous, current)
	if !applied.HasRate || applied.RateBytesPerSec != 800 || !applied.LastAdvance.Equal(lastAdvance) {
		t.Fatalf("carried sample = %+v", applied)
	}
	// The hourly startup probe kills the container: the count changes and the
	// live cursor rewinds, but the rate remains useful for another 20 minutes.
	current.Restarts = 2
	current.Position = 0
	current.Live = 0
	if rate, advanced, carried := replayRate(previous, current); rate != 800 || advanced || !carried {
		t.Fatalf("post-restart rate = %.0f, %v, %v; want 800, false, true", rate, advanced, carried)
	}
}

func TestReplayRateDropsWhenLastAdvanceIsStale(t *testing.T) {
	now := time.Now()
	previous := WALReplayProgress{
		Pod: "orders-db-2", Position: 1000, Live: 1000, Restarts: 1, At: now.Add(-21 * time.Minute),
		LastAdvance: now.Add(-21 * time.Minute), HasRate: true, RateBytesPerSec: 800,
	}
	current := WALReplayProgress{Pod: "orders-db-2", Position: 2000, Live: 2000, Restarts: 2, At: now}
	if rate, advanced, carried := replayRate(previous, current); rate != 0 || advanced || carried {
		t.Fatalf("stale rate = %.0f, %v, %v; want 0, false, false", rate, advanced, carried)
	}
	applied := applyReplayRate(previous, current)
	if applied.HasRate || !applied.LastAdvance.IsZero() {
		t.Fatalf("stale sample should drop rate and advance time: %+v", applied)
	}
	previous.LastAdvance = now.Add(-20 * time.Minute)
	if rate, advanced, carried := replayRate(previous, current); rate != 800 || advanced || !carried {
		t.Fatalf("rate at the 20 minute boundary = %.0f, %v, %v", rate, advanced, carried)
	}
}

func TestReplayRateNeverMeasuresRestartRewind(t *testing.T) {
	now := time.Now()
	previous := WALReplayProgress{Pod: "orders-db-2", Position: 100000, Live: 100000, Restarts: 1, At: now}
	current := WALReplayProgress{Pod: "orders-db-2", Position: 10, Live: 10, Restarts: 2, At: now.Add(4 * time.Second)}
	if rate, advanced, carried := replayRate(previous, current); rate != 0 || advanced || carried {
		t.Fatalf("restart rewind produced a rate: %.0f, %v, %v", rate, advanced, carried)
	}
	current.Restarts = previous.Restarts
	current.At = previous.At
	if _, advanced, _ := replayRate(previous, current); advanced {
		t.Fatal("zero elapsed time must not produce a fresh rate")
	}
	current.Pod = "other"
	current.At = previous.At.Add(time.Second)
	current.Position = previous.Position + 100
	if _, advanced, _ := replayRate(previous, current); advanced {
		t.Fatal("a different pod must not produce a fresh rate")
	}
	previous.HasRate = true
	previous.LastAdvance = now
	if _, _, carried := replayRate(previous, current); carried {
		t.Fatal("a different pod must not carry a rate either")
	}
}

func TestReplayRestartsUsesPostgresContainer(t *testing.T) {
	pod := replayTestPod(t, `{"status":{"containerStatuses":[{"name":"instance-manager","restartCount":5},{"name":"postgres","restartCount":2}]}}`)
	if got := replayRestarts(pod); got != 2 {
		t.Fatalf("restarts = %d, want 2", got)
	}
	pod = replayTestPod(t, `{"status":{"containerStatuses":[{"restartCount":5},{"restartCount":2}]}}`)
	if got := replayRestarts(pod); got != 7 {
		t.Fatalf("fallback restarts = %d, want 7", got)
	}
}
