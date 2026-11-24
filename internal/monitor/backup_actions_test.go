package monitor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestResolveBackupMethodPrefersPlugin(t *testing.T) {
	var c ClusterResource
	if err := json.Unmarshal([]byte(`{"spec":{"backup":{"barmanObjectStore":{"destinationPath":"s3://b/"},"volumeSnapshot":{"className":"csi"}},"plugins":[{"name":"barman-cloud.cloudnative-pg.io"}]}}`), &c); err != nil {
		t.Fatal(err)
	}
	method, plugin, ok, _ := ResolveBackupMethod(c)
	if !ok || method != "plugin" || plugin != "barman-cloud.cloudnative-pg.io" {
		t.Fatalf("expected plugin method, got %q %q ok=%v", method, plugin, ok)
	}
}

func TestResolveBackupMethodFallsBack(t *testing.T) {
	var c ClusterResource
	if err := json.Unmarshal([]byte(`{"spec":{"backup":{"barmanObjectStore":{"destinationPath":"s3://b/"}}}}`), &c); err != nil {
		t.Fatal(err)
	}
	method, _, ok, _ := ResolveBackupMethod(c)
	if !ok || method != "barmanObjectStore" {
		t.Fatalf("expected barmanObjectStore, got %q ok=%v", method, ok)
	}
	var v ClusterResource
	if err := json.Unmarshal([]byte(`{"spec":{"backup":{"volumeSnapshot":{"className":"csi"}}}}`), &v); err != nil {
		t.Fatal(err)
	}
	method, _, ok, _ = ResolveBackupMethod(v)
	if !ok || method != "volumeSnapshot" {
		t.Fatalf("expected volumeSnapshot, got %q ok=%v", method, ok)
	}
}

func TestResolveBackupMethodNeedsConfig(t *testing.T) {
	var c ClusterResource
	if err := json.Unmarshal([]byte(`{"spec":{}}`), &c); err != nil {
		t.Fatal(err)
	}
	_, _, ok, reason := ResolveBackupMethod(c)
	if ok || reason == "" {
		t.Fatalf("expected unsupported, got ok=%v reason=%q", ok, reason)
	}
}

func TestGenerateBackupNameUnique(t *testing.T) {
	a := GenerateBackupName("pg-cluster", time.Date(2026, 10, 6, 3, 57, 48, 0, time.UTC))
	b := GenerateBackupName("pg-cluster", time.Date(2026, 10, 6, 3, 57, 49, 0, time.UTC))
	if a == b || !strings.HasPrefix(a, "pg-cluster-") {
		t.Fatalf("names must be unique with cluster prefix: %q %q", a, b)
	}
	if len(a) > 63 {
		t.Fatalf("name must fit DNS label: %q", a)
	}
}

func TestBuildBackupManifestSetsMethod(t *testing.T) {
	raw, err := BuildBackupManifest("pg-cluster-20261006035748", "pg-cluster", "plugin", "barman-cloud.cloudnative-pg.io")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["apiVersion"] != "postgresql.cnpg.io/v1" || m["kind"] != "Backup" {
		t.Fatalf("wrong apiVersion/kind: %v", m)
	}
	spec := m["spec"].(map[string]any)
	if spec["method"] != "plugin" {
		t.Fatalf("method missing: %v", spec)
	}
	cluster := spec["cluster"].(map[string]any)
	if cluster["name"] != "pg-cluster" {
		t.Fatalf("cluster missing: %v", spec)
	}
	pc := spec["pluginConfiguration"].(map[string]any)
	if pc["name"] != "barman-cloud.cloudnative-pg.io" {
		t.Fatalf("plugin missing: %v", spec)
	}
}

func TestRecentBackupsKeepsNewest(t *testing.T) {
	mk := func(name string, hour int) BackupResource {
		var b BackupResource
		b.Metadata.Name = name
		b.Metadata.CreationTimestamp = time.Date(2026, 9, 20, hour, 0, 0, 0, time.UTC)
		return b
	}
	got := RecentBackups([]BackupResource{mk("a", 1), mk("b", 3), mk("c", 2), mk("d", 5), mk("e", 4), mk("f", 0)}, BackupHistoryLimit)
	if len(got) != 5 || got[0].Metadata.Name != "d" || got[4].Metadata.Name != "a" {
		t.Fatalf("expected newest five d,e,b,c,a got %v", namesOf(got))
	}
	if got := RecentBackups([]BackupResource{mk("only", 1)}, 5); len(got) != 1 {
		t.Fatal("fewer than the limit must stay intact")
	}
}

func namesOf(backups []BackupResource) []string {
	out := make([]string, len(backups))
	for i, b := range backups {
		out[i] = b.Metadata.Name
	}
	return out
}

func TestCanDeleteBlocksActive(t *testing.T) {
	var b BackupResource
	b.Metadata.Name = "run-1"
	b.Status.Phase = "running"
	if ok, _ := CanDeleteBackup(b); ok {
		t.Fatal("running backup must not be deletable")
	}
	b.Status.Phase = "completed"
	if ok, _ := CanDeleteBackup(b); !ok {
		t.Fatal("completed backup must be deletable")
	}
}

func TestCanStopDisabledWithExplanation(t *testing.T) {
	var b BackupResource
	b.Metadata.Name = "run-1"
	b.Status.Phase = "running"
	b.Spec.Method = "plugin"
	if ok, reason := CanStopBackup(b); ok || reason == "" {
		t.Fatalf("stop must be disabled, got ok=%v reason=%q", ok, reason)
	}
}

func TestValidateBackupIdentity(t *testing.T) {
	var sel BackupResource
	sel.Metadata.Name = "b1"
	sel.Metadata.UID = "uid-1"
	sel.Spec.Cluster.Name = "pg"
	sel.Status.Phase = "completed"
	var fresh BackupResource
	fresh.Metadata.Name = "b1"
	fresh.Metadata.UID = "uid-2"
	fresh.Spec.Cluster.Name = "pg"
	fresh.Status.Phase = "completed"
	if err := ValidateBackupIdentity(sel, fresh); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("UID change must fail: %v", err)
	}
	fresh.Metadata.UID = "uid-1"
	fresh.Spec.Cluster.Name = "other"
	if err := ValidateBackupIdentity(sel, fresh); err == nil {
		t.Fatal("cluster change must fail")
	}
	fresh.Spec.Cluster.Name = "pg"
	if err := ValidateBackupIdentity(sel, fresh); err != nil {
		t.Fatalf("same identity must pass: %v", err)
	}
}

func TestCanStartRequiresFreshData(t *testing.T) {
	var s Snapshot
	s.ResourceErr = "pods: timeout"
	s.KubeAt = time.Now()
	var c ClusterResource
	_ = json.Unmarshal([]byte(`{"spec":{"backup":{"barmanObjectStore":{"destinationPath":"s3://b/"}}}}`), &c)
	s.Cluster = c
	if ok, _ := CanStartBackup(s); ok {
		t.Fatal("stale data must block start")
	}
	s.ResourceErr = ""
	s.KubeAt = time.Now()
	if ok, _, _ := StartBackupPlan(s); !ok {
		t.Fatal("fresh data with config must allow start")
	}
}
