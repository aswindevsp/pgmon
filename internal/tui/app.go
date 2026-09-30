package tui

import (
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

const (
	refreshEvery = 5 * time.Second
	kubeEvery    = 15 * time.Second
)

type model struct {
	cfg           monitor.Config
	data          monitor.Snapshot
	busy          bool
	sqlBusy       bool
	kubeBusy      bool
	width, height int
	focus         int
	selectedPod   string
	scroll        int
	started       bool
	row           int
	detail        bool
	help          bool
	filter        string
	filtering     bool
	sortCalls     bool
}

type refreshMsg struct {
	data      monitor.Snapshot
	err       error
	Sql, kube bool
}
type tickMsg time.Time

func (m model) Init() tea.Cmd {
	return func() tea.Msg { return tickMsg(time.Now()) }
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) startCollects(Sql, kube bool) (model, tea.Cmd) {
	var cmds []tea.Cmd
	if Sql && !m.sqlBusy {
		m.sqlBusy = true
		cmds = append(cmds, collectSQLCmd(m.cfg, m.data))
	}
	if kube && !m.kubeBusy {
		m.kubeBusy = true
		cmds = append(cmds, collectKubeCmd(m.cfg, m.data))
	}
	m.busy = m.sqlBusy || m.kubeBusy
	return m, tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampScroll()
		return m, nil
	case tickMsg:
		m, cmd := m.startCollects(true, !m.started || time.Since(m.data.KubeAt) >= kubeEvery)
		return m, tea.Batch(cmd, tickCmd())
	case refreshMsg:
		m.started = true
		if msg.err == nil {
			if msg.Sql {
				applySQL(&m.data, msg.data)
			}
			if msg.kube {
				applyKube(&m.data, msg.data)
			}
		}
		if msg.Sql {
			m.sqlBusy = false
		}
		if msg.kube {
			m.kubeBusy = false
		}
		m.busy = m.sqlBusy || m.kubeBusy
		m.clampScroll()
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.filtering {
			switch msg.String() {
			case "enter", "esc":
				m.filtering = false
			case "backspace", "ctrl+h":
				r := []rune(m.filter)
				if len(r) > 0 {
					m.filter = string(r[:len(r)-1])
				}
			default:
				if msg.Type == tea.KeyRunes {
					m.filter += string(msg.Runes)
				}
			}
			m.row, m.scroll = 0, 0
			return m, nil
		}
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "r":
			var cmd tea.Cmd
			m, cmd = m.startCollects(true, true)
			m.clampScroll()
			return m, cmd
		case "tab", "right":
			m.focus = (m.focus + 1) % 6
			m.scroll, m.row, m.detail = 0, 0, false
		case "shift+tab", "left":
			m.focus = (m.focus + 5) % 6
			m.scroll, m.row, m.detail = 0, 0, false
		case "down", "j":
			if m.focus == 5 {
				m.movePod(1)
				m.scroll = 0
			} else {
				m.row++
				m.scroll++
			}
		case "up", "k":
			if m.focus == 5 {
				m.movePod(-1)
				m.scroll = 0
			} else {
				m.row = max(0, m.row-1)
				m.scroll = max(0, m.scroll-1)
			}
		case "enter":
			if m.focus == 0 {
				m.focus = 5
				m.scroll = 0
			} else {
				m.detail = !m.detail
				m.scroll = 0
			}
		case "?":
			m.help = !m.help
			m.scroll = 0
		case "/":
			m.filtering = true
			m.scroll, m.row = 0, 0
		case "esc":
			m.filter = ""
			m.detail, m.help = false, false
			m.scroll, m.row = 0, 0
		case "s":
			m.sortCalls = !m.sortCalls
			m.row = 0
		case "home":
			m.scroll, m.row = 0, 0
		case "end":
			m.scroll = 1 << 20
		case "pgdown", "space":
			m.scroll += max(1, m.height-9)
		case "pgup":
			m.scroll = max(0, m.scroll-max(1, m.height-9))
		default:
			if n, err := strconv.Atoi(msg.String()); err == nil && n >= 1 && n <= 6 {
				m.focus, m.scroll, m.row, m.detail = n-1, 0, 0, false
			}
		}
	}
	m.clampScroll()
	return m, nil
}

func collectSQLCmd(cfg monitor.Config, previous monitor.Snapshot) tea.Cmd {
	return func() tea.Msg {
		s := previous
		monitor.CollectClusterSQL(cfg, &s)
		return refreshMsg{data: s, Sql: true}
	}
}

func collectKubeCmd(cfg monitor.Config, previous monitor.Snapshot) tea.Cmd {
	return func() tea.Msg {
		s := previous
		monitor.CollectKube(cfg, &s)
		return refreshMsg{data: s, kube: true}
	}
}

func applySQL(dst *monitor.Snapshot, src monitor.Snapshot) {
	dst.Cluster = src.Cluster
	dst.Sql = src.Sql
	dst.SqlAt = src.SqlAt
	dst.SqlErr = src.SqlErr
	dst.SqlPrimary = src.SqlPrimary
	dst.WalRate = src.WalRate
	dst.WalHistory = src.WalHistory
	dst.TxRate = src.TxRate
	dst.TxHistory = src.TxHistory
	dst.ReplicationLagHistory = src.ReplicationLagHistory
	dst.BaseBackupRates = src.BaseBackupRates
}

func applyKube(dst *monitor.Snapshot, src monitor.Snapshot) {
	dst.KubeAt = src.KubeAt
	dst.ResourceErr = src.ResourceErr
	dst.Pods = src.Pods
	dst.Pvcs = src.Pvcs
	dst.Backups = src.Backups
	dst.BackupProgress = src.BackupProgress
	dst.Jobs = src.Jobs
	dst.RestoreProgress = src.RestoreProgress
	dst.Schedules = src.Schedules
	dst.Events = src.Events
}

// sqlLive reports whether the last PostgreSQL sample still describes the
// current primary.
func (m model) sqlLive() bool {
	if m.data.SqlAt.IsZero() || m.data.SqlErr != "" || m.data.SqlPrimary != m.data.Cluster.Status.CurrentPrimary {
		return false
	}
	age := time.Since(m.data.SqlAt)
	if age <= 2*refreshEvery {
		return true
	}
	// A kubectl-backed refresh can outlast the 10s display window. Keep the last
	// good sample live while that collect is still running, but not indefinitely.
	return m.sqlBusy && age <= time.Minute
}

// viewContext builds the read-only input the tab packages render from.
func (m model) viewContext() shared.Context {
	return shared.Context{
		Data:         m.data,
		SelectedPod:  m.selectedPod,
		SQLLive:      m.sqlLive(),
		KubeBusy:     m.kubeBusy,
		Started:      m.started,
		Filter:       m.filter,
		SortCalls:    m.sortCalls,
		Detail:       m.detail,
		Row:          m.row,
		RefreshEvery: refreshEvery,
		KubeEvery:    kubeEvery,
	}
}
