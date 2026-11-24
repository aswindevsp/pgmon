package monitor

import (
	"errors"
	"testing"
)

func TestDeleteGuardRequiresInactiveAndSameIdentity(t *testing.T) {
	sel := BackupResource{}
	sel.Metadata.Name = "b1"
	sel.Metadata.UID = "uid-1"
	sel.Spec.Cluster.Name = "pg"
	sel.Status.Phase = "running"
	fresh := sel
	fresh.Status.Phase = "running"
	if err := DeleteBackupGuard(sel, fresh); err == nil || !contains(err.Error(), "no longer active") && !contains(err.Error(), "stop") {
		t.Fatalf("running must block delete: %v", err)
	}
	sel.Status.Phase = "completed"
	fresh.Status.Phase = "completed"
	if err := DeleteBackupGuard(sel, fresh); err != nil {
		t.Fatalf("completed same identity must pass: %v", err)
	}
	fresh.Metadata.UID = "uid-2"
	if err := DeleteBackupGuard(sel, fresh); err == nil {
		t.Fatal("UID reuse must fail")
	}
}

func TestFormatKubectlErrorIsActionable(t *testing.T) {
	err := formatKubectlError("create", []string{"create", "-f", "-"}, "backups.postgresql.cnpg.io is forbidden")
	if err == nil || !contains(err.Error(), "create") || !contains(err.Error(), "forbidden") {
		t.Fatalf("error must carry action and server message: %v", err)
	}
	var target *KubectlError
	if !errors.As(err, &target) {
		t.Fatal("expected KubectlError")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
