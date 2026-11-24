package shared

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"postgres-monitor/internal/monitor"
)

// Context is the read-only input assembled by the application model for each
// render. Views must not mutate it.
type Context struct {
	Data         monitor.Snapshot
	SelectedPod  string
	SQLLive      bool
	KubeBusy     bool
	Started      bool
	Filter       string
	SortCalls    bool
	Detail       bool
	Row          int
	RefreshEvery time.Duration
	KubeEvery    time.Duration
	// Action state. Views render only; all writes stay in internal/tui.
	KubeContext              string
	Namespace                string
	Cluster                  string
	BackupSelected           string
	BackupNotice             string
	BackupBusy               bool
	BackupConfirmKind        string
	BackupConfirmName        string
	BackupConfirmMeth        string
	BackupConfirmPlug        string
	BackupConfirmPhase       string
	SwitchoverNotice         string
	SwitchoverBusy           bool
	SwitchoverPending        string
	SwitchoverConfirmPrimary string
	SwitchoverConfirmTarget  string
}

// Node is a topology entry: the primary or a pod that should appear as a replica.
type Node struct {
	Name    string
	Pod     *monitor.PodResource
	Primary bool
}

// PrimaryName reports the current or target primary, if known.
func (c Context) PrimaryName() string {
	if c.Data.Cluster.Status.CurrentPrimary != "" {
		return c.Data.Cluster.Status.CurrentPrimary
	}
	return c.Data.Cluster.Status.TargetPrimary
}

// Nodes shapes the snapshot into primary and replica entries for topology views.
func (c Context) Nodes() []Node {
	primary := c.PrimaryName()
	nodes := []Node{{Name: ValueOr(primary, "Primary not yet reported"), Primary: true}}
	for i := range c.Data.Pods {
		p := &c.Data.Pods[i]
		job := false
		for _, owner := range p.Metadata.OwnerReferences {
			job = job || owner.Kind == "Job"
		}
		if job {
			if monitor.ClusterRecovering(c.Data) && p.Metadata.Labels["cnpg.io/jobRole"] == "full-recovery" && nodes[0].Pod == nil {
				if primary == "" || p.Metadata.Labels["cnpg.io/instanceName"] == primary {
					nodes[0].Pod = p
				}
			}
			continue
		}
		if p.Metadata.Name == primary {
			nodes[0].Pod = p
		} else {
			nodes = append(nodes, Node{Name: p.Metadata.Name, Pod: p})
		}
	}
	sort.Slice(nodes[1:], func(i, j int) bool { return nodes[i+1].Name < nodes[j+1].Name })
	return nodes
}

// SelectedIndex returns the position of name in nodes, defaulting to zero.
func SelectedIndex(nodes []Node, name string) int {
	for i, n := range nodes {
		if n.Name == name {
			return i
		}
	}
	return 0
}

// Replication returns the primary's measurement for name when SQL data is live.
func (c Context) Replication(name string) *monitor.ReplicationStatus {
	if !c.SQLLive {
		return nil
	}
	for i := range c.Data.Sql.Replication {
		if c.Data.Sql.Replication[i].ApplicationName == name {
			return &c.Data.Sql.Replication[i]
		}
	}
	return nil
}

// ReplayLag renders the reported replay lag for a replica.
func ReplayLag(r *monitor.ReplicationStatus) string {
	if r.ReplayLag == nil || *r.ReplayLag == "" {
		return "—"
	}
	return IntervalShort(*r.ReplayLag)
}

// PodHealth reports a pod's readiness label and display style.
func PodHealth(p *monitor.PodResource) (string, lipgloss.Style) {
	if p == nil {
		return "? Pod data unavailable", Muted
	}
	if p.Status.Phase == "Failed" || p.Status.Phase == "Unknown" {
		return "× " + p.Status.Phase, Red
	}
	if p.Status.Phase != "Running" {
		return "! " + ValueOr(p.Status.Phase, "Pending"), Amber
	}
	ready := len(p.Status.ContainerStatuses) > 0
	for _, c := range p.Status.ContainerStatuses {
		ready = ready && c.Ready
	}
	if !ready {
		return "! Running · not ready", Amber
	}
	return "● Running · ready", Green
}

// Restarts totals container restarts for a pod.
func Restarts(p *monitor.PodResource) int {
	count := 0
	if p != nil {
		for _, c := range p.Status.ContainerStatuses {
			count += c.RestartCount
		}
	}
	return count
}

// FilteredSessions returns collected sessions matching the active filter.
func FilteredSessions(ctx Context) []monitor.SessionStatus {
	rows := []monitor.SessionStatus{}
	for _, s := range ctx.Data.Sql.Sessions {
		text := fmt.Sprintf("%d %s %s %s %s %s", s.PID, s.Database, s.User, s.Application, s.State, s.Query)
		if strings.Contains(strings.ToLower(text), strings.ToLower(ctx.Filter)) {
			rows = append(rows, s)
		}
	}
	return rows
}
