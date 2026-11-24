package monitor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const backupPluginName = "barman-cloud.cloudnative-pg.io"

// ResolveBackupMethod reports the configured backup method for a cluster.
// It prefers the Barman Cloud plugin when present, then barmanObjectStore,
// then volumeSnapshot. It never infers the method from backup history.
func ResolveBackupMethod(c ClusterResource) (method, pluginName string, ok bool, reason string) {
	for _, p := range c.Spec.Plugins {
		if p.Name == backupPluginName {
			return "plugin", p.Name, true, ""
		}
	}
	// Any other plugin managing backups is still a plugin backup, but we
	// only advertise the verified Barman Cloud plugin name.
	for _, p := range c.Spec.Plugins {
		if p.Name != "" {
			return "plugin", p.Name, true, ""
		}
	}
	if c.Spec.Backup != nil {
		if c.Spec.Backup.BarmanObjectStore != nil {
			return "barmanObjectStore", "", true, ""
		}
		if c.Spec.Backup.VolumeSnapshot != nil {
			return "volumeSnapshot", "", true, ""
		}
	}
	return "", "", false, "no supported backup configuration on cluster (need backup.barmanObjectStore, backup.volumeSnapshot, or barman-cloud plugin)"
}

// GenerateBackupName builds a unique DNS-1035 backup name for a cluster.
func GenerateBackupName(cluster string, now time.Time) string {
	base := strings.ToLower(cluster)
	base = strings.ReplaceAll(base, "_", "-")
	stamp := now.UTC().Format("20060102150405")
	name := fmt.Sprintf("%s-%s", base, stamp)
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.Trim(name, "-")
}

// BuildBackupManifest returns JSON for kubectl create -f -.
func BuildBackupManifest(backupName, clusterName, method, pluginName string) ([]byte, error) {
	manifest := map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Backup",
		"metadata": map[string]any{
			"name": backupName,
		},
		"spec": map[string]any{
			"cluster": map[string]any{"name": clusterName},
			"method":  method,
		},
	}
	if method == "plugin" {
		manifest["spec"].(map[string]any)["pluginConfiguration"] = map[string]any{"name": pluginName}
	}
	return json.Marshal(manifest)
}

// BackupHistoryLimit is how many backups the Backups panel lists.
const BackupHistoryLimit = 5

// OrderedBackups sorts newest first by observed start, matching history.
func OrderedBackups(backups []BackupResource) []BackupResource {
	out := append([]BackupResource(nil), backups...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && BackupStart(out[j]).After(BackupStart(out[j-1])); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// RecentBackups returns the newest limit backups, newest first.
func RecentBackups(backups []BackupResource, limit int) []BackupResource {
	out := OrderedBackups(backups)
	if limit <= 0 || len(out) <= limit {
		return out
	}
	return out[:limit]
}

// FindBackupByName locates a backup resource by name.
func FindBackupByName(backups []BackupResource, name string) (BackupResource, bool) {
	for _, b := range backups {
		if b.Metadata.Name == name {
			return b, true
		}
	}
	return BackupResource{}, false
}

// CanStartBackup gates Start on supported config and fresh Kubernetes data.
func CanStartBackup(s Snapshot) (bool, string) {
	ok, _, _, reason := startPlan(s)
	if !ok {
		return false, reason
	}
	return true, ""
}

// StartBackupPlan resolves the method for a start request.
func StartBackupPlan(s Snapshot) (bool, string, string) {
	ok, method, plugin, _ := startPlan(s)
	return ok, method, plugin
}

func startPlan(s Snapshot) (bool, string, string, string) {
	if s.ResourceErr != "" {
		return false, "", "", "Kubernetes data may be stale: " + s.ResourceErr
	}
	if s.KubeAt.IsZero() {
		return false, "", "", "Kubernetes data not yet collected"
	}
	method, plugin, ok, reason := ResolveBackupMethod(s.Cluster)
	if !ok {
		return false, "", "", reason
	}
	return true, method, plugin, ""
}

// CanStopBackup is intentionally disabled: no verified cancellation path for
// any backup method has been established. Deleting the Backup object does not
// stop the worker, and killing pods/processes is not a substitute.
func CanStopBackup(b BackupResource) (bool, string) {
	if !ActiveBackup(b) {
		return false, "backup is not running"
	}
	return false, "stop is unavailable: no verified cancellation path for method " + backupMethodName(b) + "; deleting the Backup resource does not stop the worker"
}

func backupMethodName(b BackupResource) string {
	if b.Status.Method != "" {
		return b.Status.Method
	}
	if b.Spec.Method != "" {
		return b.Spec.Method
	}
	return "unknown"
}

// CanDeleteBackup allows deletion only when the backup is no longer active.
func CanDeleteBackup(b BackupResource) (bool, string) {
	if b.Metadata.Name == "" {
		return false, "no backup selected"
	}
	if ActiveBackup(b) {
		return false, "stop the running backup and confirm it is no longer active before deleting"
	}
	return true, ""
}

// ValidateBackupIdentity ensures a fresh observation still targets the same object.
func ValidateBackupIdentity(selected, fresh BackupResource) error {
	if fresh.Metadata.Name == "" {
		return fmt.Errorf("backup %q no longer exists; select a backup again", selected.Metadata.Name)
	}
	if selected.Metadata.UID != "" && fresh.Metadata.UID != "" && selected.Metadata.UID != fresh.Metadata.UID {
		return fmt.Errorf("backup %q changed (UID mismatch); select again", selected.Metadata.Name)
	}
	if selected.Spec.Cluster.Name != "" && fresh.Spec.Cluster.Name != "" && selected.Spec.Cluster.Name != fresh.Spec.Cluster.Name {
		return fmt.Errorf("backup %q now targets cluster %q; select again", selected.Metadata.Name, fresh.Spec.Cluster.Name)
	}
	return nil
}
