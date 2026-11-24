package monitor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// KubectlError carries an actionable kubectl failure.
type KubectlError struct {
	Action string
	Args   []string
	Output string
}

func (e *KubectlError) Error() string {
	if e.Output != "" {
		return fmt.Sprintf("%s failed: %s", e.Action, e.Output)
	}
	return fmt.Sprintf("%s failed", e.Action)
}

func formatKubectlError(action string, args []string, output string) error {
	return &KubectlError{Action: action, Args: append([]string(nil), args...), Output: strings.TrimSpace(output)}
}

// runKubectlStdin runs kubectl with fixed context/namespace, feeding stdin when non-nil.
func runKubectlStdin(cfg Config, stdin []byte, timeout time.Duration, args ...string) ([]byte, error) {
	full := append([]string{"--context", cfg.Context, "-n", cfg.Namespace}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", full...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, formatKubectlError(strings.Join(args[:min(2, len(args))], " "), args, "kubectl timed out")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, formatKubectlError(strings.Join(args[:min(2, len(args))], " "), args, msg)
	}
	return bytes.TrimSpace(out), nil
}

// ApplyBackupManifest creates one Backup resource from JSON/YAML via kubectl create.
func ApplyBackupManifest(cfg Config, manifest []byte) (string, error) {
	out, err := runKubectlStdin(cfg, manifest, 15*time.Second, "create", "-f", "-")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// DeleteBackupObject removes the named Backup resource.
func DeleteBackupObject(cfg Config, name string) (string, error) {
	if name == "" {
		return "", formatKubectlError("delete", []string{"delete", "backup"}, "no backup name")
	}
	out, err := runKubectlStdin(cfg, nil, 15*time.Second, "delete", "backups.postgresql.cnpg.io", name)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// FetchBackup re-reads a Backup resource immediately before stop/delete.
func FetchBackup(cfg Config, name string) (BackupResource, error) {
	var b BackupResource
	if err := GetJSON(cfg, &b, "get", "backups.postgresql.cnpg.io", name, "-o", "json"); err != nil {
		return BackupResource{}, err
	}
	return b, nil
}

// DeleteBackupGuard enforces delete gating plus identity continuity.
func DeleteBackupGuard(selected, fresh BackupResource) error {
	if ok, reason := CanDeleteBackup(fresh); !ok {
		// If the fresh observation is still active, block even when the
		// selection snapshot was terminal: the worker has not stopped.
		if ActiveBackup(fresh) {
			return fmt.Errorf("cannot delete running backup %q: %s", fresh.Metadata.Name, reason)
		}
		// Fall back to the selected state for missing/terminal mismatches.
		if selected.Metadata.Name == "" {
			return fmt.Errorf("cannot delete backup: %s", reason)
		}
		return fmt.Errorf("cannot delete backup %q: %s", selected.Metadata.Name, reason)
	}
	if err := ValidateBackupIdentity(selected, fresh); err != nil {
		return err
	}
	return nil
}
