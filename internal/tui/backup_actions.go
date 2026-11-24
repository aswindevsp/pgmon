package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"pgmon/internal/monitor"
)

type backupConfirm struct {
	kind   string // "start" or "delete"
	name   string // target name for delete; "" for start
	method string
	plugin string
	uid    string
	phase  string
}

type backupCreateResult struct {
	name   string
	output string
	err    error
}

type backupDeleteResult struct {
	name   string
	output string
	err    error
}

func orderedBackupNames(backups []monitor.BackupResource) []string {
	ordered := monitor.RecentBackups(backups, monitor.BackupHistoryLimit)
	names := make([]string, 0, len(ordered))
	for _, b := range ordered {
		names = append(names, b.Metadata.Name)
	}
	return names
}

func nextBackupSelection(backups []monitor.BackupResource, current string, delta int) string {
	names := orderedBackupNames(backups)
	if len(names) == 0 {
		return ""
	}
	if current == "" {
		return names[0]
	}
	idx := -1
	for i, n := range names {
		if n == current {
			idx = i
			break
		}
	}
	if idx < 0 {
		return names[0]
	}
	idx = (idx + delta + len(names)) % len(names)
	return names[idx]
}

func reconcileBackupSelection(backups []monitor.BackupResource, current string) string {
	if len(backups) == 0 {
		return ""
	}
	names := orderedBackupNames(backups)
	if current == "" {
		return names[0]
	}
	for _, n := range names {
		if n == current {
			return current
		}
	}
	if _, ok := monitor.FindBackupByName(backups, current); ok {
		return names[0]
	}
	return ""
}

// reconcileAfterKube pins selection by identity and keeps any open
// confirmation so refreshes arriving while a prompt is open do not dismiss it.
func (m model) reconcileAfterKube() model {
	m.backupSel = reconcileBackupSelection(m.data.Backups, m.backupSel)
	return m
}

func (m *model) ensureBackupSel() {
	if m.backupSel == "" {
		m.backupSel = reconcileBackupSelection(m.data.Backups, "")
	}
}

func (m *model) openStartConfirm() {
	ok, method, plugin := monitor.StartBackupPlan(m.data)
	if !ok {
		_, reason := monitor.CanStartBackup(m.data)
		m.backupNotice = "Cannot start backup: " + reason
		return
	}
	m.backupConfirm = &backupConfirm{kind: "start", method: method, plugin: plugin}
	m.backupNotice = ""
}

func (m *model) openDeleteConfirm() {
	m.ensureBackupSel()
	if m.backupSel == "" {
		m.backupNotice = "No backup selected"
		return
	}
	sel, ok := monitor.FindBackupByName(m.data.Backups, m.backupSel)
	if !ok {
		m.backupSel = ""
		m.backupNotice = "Selected backup no longer exists; choose again"
		return
	}
	if can, reason := monitor.CanDeleteBackup(sel); !can {
		m.backupNotice = "Cannot delete " + sel.Metadata.Name + ": " + reason
		return
	}
	method := sel.Spec.Method
	if method == "" {
		method = sel.Status.Method
	}
	m.backupConfirm = &backupConfirm{kind: "delete", name: sel.Metadata.Name, uid: sel.Metadata.UID, phase: sel.Status.Phase, method: method}
	m.backupNotice = ""
}

func (m *model) handleStopKey() {
	m.ensureBackupSel()
	if m.backupSel == "" {
		m.backupNotice = "No backup selected"
		return
	}
	sel, ok := monitor.FindBackupByName(m.data.Backups, m.backupSel)
	if !ok {
		m.backupSel = ""
		m.backupNotice = "Selected backup no longer exists; choose again"
		return
	}
	_, reason := monitor.CanStopBackup(sel)
	phase := sel.Status.Phase
	if phase == "" {
		phase = "phase unknown"
	}
	m.backupNotice = "Stop unavailable for " + sel.Metadata.Name + " (" + phase + "): " + reason
}

func backupRequestNotice(c backupConfirm, cluster string) string {
	if c.kind == "start" {
		return "Creating backup for cluster " + cluster + "..."
	}
	return "Deleting Backup resource " + c.name + "..."
}

func createBackupCmd(cfg monitor.Config, method, plugin string) tea.Cmd {
	return func() tea.Msg {
		name := monitor.GenerateBackupName(cfg.Cluster, time.Now())
		manifest, err := monitor.BuildBackupManifest(name, cfg.Cluster, method, plugin)
		if err != nil {
			return backupCreateResult{name: name, err: err}
		}
		out, err := monitor.ApplyBackupManifest(cfg, manifest)
		if err != nil {
			return backupCreateResult{name: name, err: err}
		}
		return backupCreateResult{name: name, output: out}
	}
}

func deleteBackupCmd(cfg monitor.Config, selected monitor.BackupResource) tea.Cmd {
	return func() tea.Msg {
		fresh, err := monitor.FetchBackup(cfg, selected.Metadata.Name)
		if err != nil {
			return backupDeleteResult{name: selected.Metadata.Name, err: err}
		}
		if err := monitor.DeleteBackupGuard(selected, fresh); err != nil {
			return backupDeleteResult{name: selected.Metadata.Name, err: err}
		}
		if ok, reason := monitor.CanDeleteBackup(fresh); !ok {
			return backupDeleteResult{name: selected.Metadata.Name, err: errorString(reason)}
		}
		out, err := monitor.DeleteBackupObject(cfg, selected.Metadata.Name)
		if err != nil {
			return backupDeleteResult{name: selected.Metadata.Name, err: err}
		}
		return backupDeleteResult{name: selected.Metadata.Name, output: out}
	}
}

type stringError string

func (e stringError) Error() string { return string(e) }

func errorString(s string) error { return stringError(s) }
