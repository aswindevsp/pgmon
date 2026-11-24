package monitor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func switchoverSnap(t *testing.T, primary string, replicas ...string) Snapshot {
	t.Helper()
	var s Snapshot
	s.KubeAt = time.Now()
	s.Cluster.Status.CurrentPrimary = primary
	s.Cluster.Status.TargetPrimary = primary
	s.Cluster.Spec.Instances = 1 + len(replicas)
	s.Cluster.Status.ReadyInstances = s.Cluster.Spec.Instances
	addReady := func(name string) {
		var p PodResource
		raw := `{"metadata":{"name":"` + name + `"},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}`
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		s.Pods = append(s.Pods, p)
	}
	addReady(primary)
	for _, r := range replicas {
		addReady(r)
	}
	return s
}

func TestCanSwitchoverRequiresFreshReadyReplica(t *testing.T) {
	s := switchoverSnap(t, "pg-1", "pg-2")
	if ok, _ := CanSwitchover(s, "pg-2"); !ok {
		t.Fatal("ready replica must be allowed")
	}
	if ok, reason := CanSwitchover(s, "pg-1"); ok || !strings.Contains(reason, "current primary") {
		t.Fatalf("primary must be rejected: ok=%v reason=%q", ok, reason)
	}
	if ok, reason := CanSwitchover(s, ""); ok || reason == "" {
		t.Fatalf("empty target must be rejected: %q", reason)
	}
	s.ResourceErr = "pods: timeout"
	if ok, reason := CanSwitchover(s, "pg-2"); ok || !strings.Contains(reason, "stale") {
		t.Fatalf("stale kube must block: %q", reason)
	}
}

func TestCanSwitchoverBlocksRecoveringAndInProgress(t *testing.T) {
	s := switchoverSnap(t, "pg-1", "pg-2")
	s.Cluster.Status.CurrentPrimary = ""
	s.Cluster.Status.TargetPrimary = "pg-1"
	s.Cluster.Status.Phase = "Setting up primary"
	if ok, reason := CanSwitchover(s, "pg-2"); ok || !strings.Contains(reason, "recover") {
		t.Fatalf("recovery must block: %q", reason)
	}
	s = switchoverSnap(t, "pg-1", "pg-2")
	s.Cluster.Status.TargetPrimary = "pg-2"
	if !SwitchoverInProgress(s.Cluster) {
		t.Fatal("expected in-progress switchover")
	}
	if ok, reason := CanSwitchover(s, "pg-2"); ok || !strings.Contains(reason, "in progress") {
		t.Fatalf("in-progress must block: %q", reason)
	}
}

func TestCanSwitchoverRejectsMissingUnreadyAndJobs(t *testing.T) {
	s := switchoverSnap(t, "pg-1", "pg-2")
	if ok, reason := CanSwitchover(s, "pg-9"); ok || !strings.Contains(reason, "not a cluster pod") {
		t.Fatalf("missing instance: %q", reason)
	}
	s.Pods[1].Status.ContainerStatuses[0].Ready = false
	if ok, reason := CanSwitchover(s, "pg-2"); ok || !strings.Contains(reason, "not ready") {
		t.Fatalf("unready: %q", reason)
	}
	s.Pods[1].Status.ContainerStatuses[0].Ready = true
	s.Pods[1].Status.Phase = "Pending"
	if ok, reason := CanSwitchover(s, "pg-2"); ok || !strings.Contains(reason, "not running") {
		t.Fatalf("pending: %q", reason)
	}
	var job PodResource
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"pg-join","ownerReferences":[{"kind":"Job"}]},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}`), &job); err != nil {
		t.Fatal(err)
	}
	s.Pods = append(s.Pods, job)
	if _, ok := FindInstance(s.Pods, "pg-join"); ok {
		t.Fatal("job pods are not switchover targets")
	}
}

func TestValidateSwitchoverDetectsPrimaryChange(t *testing.T) {
	fresh := ClusterResource{}
	fresh.Status.CurrentPrimary = "pg-2"
	fresh.Status.TargetPrimary = "pg-2"
	if err := ValidateSwitchover("pg-1", "pg-2", fresh); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("primary change must fail: %v", err)
	}
	fresh.Status.CurrentPrimary = "pg-1"
	fresh.Status.TargetPrimary = "pg-1"
	if err := ValidateSwitchover("pg-1", "pg-2", fresh); err != nil {
		t.Fatalf("stable pair must pass: %v", err)
	}
	fresh.Status.TargetPrimary = "pg-3"
	if err := ValidateSwitchover("pg-1", "pg-2", fresh); err == nil {
		t.Fatal("in-progress must fail")
	}
}

func TestSwitchoverGuardUsesFreshReadiness(t *testing.T) {
	s := switchoverSnap(t, "pg-1", "pg-2")
	if err := SwitchoverGuard("pg-1", "pg-2", s); err != nil {
		t.Fatalf("fresh ready replica must pass: %v", err)
	}
	s.Pods[1].Status.ContainerStatuses[0].Ready = false
	if err := SwitchoverGuard("pg-1", "pg-2", s); err == nil {
		t.Fatal("unready fresh pod must fail")
	}
}

func TestPromoteArgsMatchPluginDocs(t *testing.T) {
	args := PromoteArgs("cluster-example", "cluster-example-2")
	if strings.Join(args, " ") != "cnpg promote cluster-example cluster-example-2" {
		t.Fatalf("unexpected promote args: %v", args)
	}
}

func TestAnnotatePromoteErrorMentionsPlugin(t *testing.T) {
	err := annotatePromoteError(formatKubectlError("cnpg", []string{"cnpg", "promote"}, `unknown command "cnpg" for "kubectl"`))
	if err == nil || !strings.Contains(err.Error(), "kubectl cnpg plugin") {
		t.Fatalf("missing plugin hint: %v", err)
	}
}
