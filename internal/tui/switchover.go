package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"pgmon/internal/monitor"
	"pgmon/internal/tui/views/shared"
)

type switchoverConfirm struct {
	primary string
	target  string
}

type switchoverResult struct {
	primary string
	target  string
	output  string
	err     error
}

func (m *model) selectedInstance() string {
	nodes := m.viewContext().Nodes()
	if len(nodes) == 0 {
		return m.selectedPod
	}
	return nodes[shared.SelectedIndex(nodes, m.selectedPod)].Name
}

func (m *model) selectPod(delta int) {
	nodes := m.viewContext().Nodes()
	if len(nodes) == 0 {
		return
	}
	i := (shared.SelectedIndex(nodes, m.selectedPod) + delta + len(nodes)) % len(nodes)
	m.selectedPod = nodes[i].Name
}

func (m *model) openSwitchoverConfirm() {
	target := m.selectedInstance()
	ok, primary, replica := monitor.SwitchoverPlan(m.data, target)
	if !ok {
		_, reason := monitor.CanSwitchover(m.data, target)
		m.switchoverNotice = "Cannot switchover: " + reason
		return
	}
	m.switchoverConfirm = &switchoverConfirm{primary: primary, target: replica}
	m.switchoverNotice = ""
}

func (m model) confirmOpen() bool {
	return m.backupConfirm != nil || m.switchoverConfirm != nil
}

func switchoverRequestNotice(c switchoverConfirm) string {
	return "Requesting switchover " + c.primary + " → " + c.target + "..."
}

func promoteCmd(cfg monitor.Config, planned switchoverConfirm) tea.Cmd {
	return func() tea.Msg {
		cluster, err := monitor.FetchCluster(cfg)
		if err != nil {
			return switchoverResult{primary: planned.primary, target: planned.target, err: err}
		}
		pod, err := monitor.FetchPod(cfg, planned.target)
		if err != nil {
			return switchoverResult{primary: planned.primary, target: planned.target, err: err}
		}
		fresh := monitor.Snapshot{
			KubeAt:  time.Now(),
			Cluster: cluster,
			Pods:    []monitor.PodResource{pod},
		}
		if err := monitor.SwitchoverGuard(planned.primary, planned.target, fresh); err != nil {
			return switchoverResult{primary: planned.primary, target: planned.target, err: err}
		}
		out, err := monitor.PromoteInstance(cfg, planned.target)
		return switchoverResult{primary: planned.primary, target: planned.target, output: out, err: err}
	}
}

func (m *model) applySwitchoverProgress() {
	if m.switchoverPending == "" {
		return
	}
	current := m.data.Cluster.Status.CurrentPrimary
	if current == m.switchoverPending {
		m.switchoverNotice = "Primary is now " + current
		m.switchoverPending = ""
		return
	}
	if monitor.SwitchoverInProgress(m.data.Cluster) && m.data.Cluster.Status.TargetPrimary == m.switchoverPending {
		m.switchoverNotice = "Switchover in progress: " + current + " → " + m.switchoverPending
	}
}
