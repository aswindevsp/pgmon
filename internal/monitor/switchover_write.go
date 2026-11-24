package monitor

import (
	"fmt"
	"strings"
	"time"
)

const promoteTimeout = 30 * time.Second

// FetchCluster re-reads the Cluster resource immediately before switchover.
func FetchCluster(cfg Config) (ClusterResource, error) {
	var c ClusterResource
	if err := GetJSON(cfg, &c, "get", "clusters.postgresql.cnpg.io", cfg.Cluster, "-o", "json"); err != nil {
		return ClusterResource{}, err
	}
	return c, nil
}

// FetchPod re-reads one pod immediately before switchover.
func FetchPod(cfg Config, name string) (PodResource, error) {
	var p PodResource
	if name == "" {
		return PodResource{}, formatKubectlError("get", []string{"get", "pod"}, "no instance name")
	}
	if err := GetJSON(cfg, &p, "get", "pod", name, "-o", "json"); err != nil {
		return PodResource{}, err
	}
	return p, nil
}

// SwitchoverGuard re-checks identity and readiness on a fresh observation.
func SwitchoverGuard(plannedPrimary, plannedTarget string, fresh Snapshot) error {
	if err := ValidateSwitchover(plannedPrimary, plannedTarget, fresh.Cluster); err != nil {
		return err
	}
	if ok, reason := CanSwitchover(fresh, plannedTarget); !ok {
		return fmt.Errorf("%s", reason)
	}
	return nil
}

// PromoteInstance requests a graceful switchover with kubectl cnpg promote.
func PromoteInstance(cfg Config, instance string) (string, error) {
	if cfg.Cluster == "" || instance == "" {
		return "", formatKubectlError("promote", PromoteArgs(cfg.Cluster, instance), "cluster and instance are required")
	}
	out, err := runKubectlStdin(cfg, nil, promoteTimeout, PromoteArgs(cfg.Cluster, instance)...)
	if err != nil {
		return "", annotatePromoteError(err)
	}
	return string(out), nil
}

func annotatePromoteError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "unknown command") || strings.Contains(msg, "did you mean") {
		return formatKubectlError("promote", []string{"cnpg", "promote"}, msg+"; install the kubectl cnpg plugin")
	}
	return err
}
