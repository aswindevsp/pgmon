package monitor

import (
	"fmt"
	"strings"
)

// SwitchoverInProgress reports a requested primary that has not become current.
func SwitchoverInProgress(c ClusterResource) bool {
	cur, tgt := c.Status.CurrentPrimary, c.Status.TargetPrimary
	return cur != "" && tgt != "" && cur != tgt
}

// FindInstance locates a non-job cluster pod by name.
func FindInstance(pods []PodResource, name string) (PodResource, bool) {
	if name == "" {
		return PodResource{}, false
	}
	for _, p := range pods {
		if p.Metadata.Name != name || jobOwned(p) {
			continue
		}
		return p, true
	}
	return PodResource{}, false
}

func jobOwned(p PodResource) bool {
	for _, owner := range p.Metadata.OwnerReferences {
		if owner.Kind == "Job" {
			return true
		}
	}
	return false
}

func instanceReady(p PodResource) bool {
	if p.Status.Phase != "Running" {
		return false
	}
	if len(p.Status.ContainerStatuses) == 0 {
		return false
	}
	for _, c := range p.Status.ContainerStatuses {
		if !c.Ready {
			return false
		}
	}
	return true
}

// CanSwitchover gates a graceful promote of target via kubectl cnpg promote.
func CanSwitchover(s Snapshot, target string) (bool, string) {
	if s.ResourceErr != "" {
		return false, "Kubernetes data may be stale: " + s.ResourceErr
	}
	if s.KubeAt.IsZero() {
		return false, "Kubernetes data not yet collected"
	}
	if ClusterRecovering(s) {
		return false, "cluster is recovering; wait for a primary before switchover"
	}
	primary := s.Cluster.Status.CurrentPrimary
	if primary == "" {
		return false, "current primary is unknown"
	}
	if SwitchoverInProgress(s.Cluster) {
		return false, "switchover already in progress toward " + s.Cluster.Status.TargetPrimary
	}
	if target == "" {
		return false, "no instance selected"
	}
	if target == primary {
		return false, "select a replica, not the current primary " + primary
	}
	pod, ok := FindInstance(s.Pods, target)
	if !ok {
		return false, "instance " + target + " is not a cluster pod"
	}
	if pod.Status.Phase != "Running" {
		return false, "replica " + target + " is not running (" + strings.TrimSpace(pod.Status.Phase) + ")"
	}
	if !instanceReady(pod) {
		return false, "replica " + target + " is not ready"
	}
	return true, ""
}

// SwitchoverPlan resolves the current primary and selected replica for a request.
func SwitchoverPlan(s Snapshot, target string) (ok bool, primary, replica string) {
	ok, _ = CanSwitchover(s, target)
	return ok, s.Cluster.Status.CurrentPrimary, target
}

// ValidateSwitchover checks that a fresh observation still matches the planned pair.
func ValidateSwitchover(plannedPrimary, plannedTarget string, fresh ClusterResource) error {
	if plannedTarget == "" {
		return fmt.Errorf("no instance selected")
	}
	if fresh.Status.CurrentPrimary == "" {
		return fmt.Errorf("current primary is unknown; select again")
	}
	if plannedPrimary != "" && fresh.Status.CurrentPrimary != plannedPrimary {
		return fmt.Errorf("primary changed from %q to %q; select again", plannedPrimary, fresh.Status.CurrentPrimary)
	}
	if SwitchoverInProgress(fresh) {
		return fmt.Errorf("switchover already in progress toward %q", fresh.Status.TargetPrimary)
	}
	if plannedTarget == fresh.Status.CurrentPrimary {
		return fmt.Errorf("target %q is now the current primary; select a replica", plannedTarget)
	}
	return nil
}

// PromoteArgs is the kubectl plugin invocation after context and namespace flags.
func PromoteArgs(cluster, instance string) []string {
	return []string{"cnpg", "promote", cluster, instance}
}
