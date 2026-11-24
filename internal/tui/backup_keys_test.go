package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"pgmon/internal/monitor"
)

func TestBackupKeysOpenConfirms(t *testing.T) {
	m := fixtureForBackup(t)
	m.focus = 2
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(model)
	if m.backupConfirm == nil || m.backupConfirm.kind != "start" {
		t.Fatalf("n must open start confirm: %+v", m.backupConfirm)
	}
	if cmd != nil {
		t.Fatal("opening confirm must not dispatch")
	}
	m = fixtureForBackup(t)
	m.focus = 2
	m.backupSel = "old-1" // completed? actually failed in fixture is terminal
	// fixture: older completed, newer failed, run-1 running. Use older (completed).
	m.backupSel = "older"
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(model)
	if m.backupConfirm == nil || m.backupConfirm.kind != "delete" || m.backupConfirm.name != "older" {
		t.Fatalf("d must open delete confirm: %+v", m.backupConfirm)
	}
}

func TestBackupSelectionScrollsIntoView(t *testing.T) {
	m := fixtureForBackup(t)
	m.focus, m.width, m.height = 2, 80, 16
	m.data.Backups = nil
	for i := 0; i < 24; i++ {
		b := monitor.BackupResource{}
		b.Metadata.Name = fmt.Sprintf("backup-%02d", i)
		b.Metadata.CreationTimestamp = time.Date(2026, 9, 1, i, 0, 0, 0, time.UTC)
		b.Status.Phase = "completed"
		m.data.Backups = append(m.data.Backups, b)
	}
	m.backupSel = ""
	for i := 0; i < 18; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
		visible := false
		for _, line := range strings.Split(m.View(), "\n") {
			if strings.Contains(line, "› ") && strings.Contains(line, m.backupSel) {
				visible = true
				break
			}
		}
		if !visible {
			t.Fatalf("selected backup %q is outside viewport at scroll %d", m.backupSel, m.scroll)
		}
	}
}

func TestDeleteBlockedForRunning(t *testing.T) {
	m := fixtureForBackup(t)
	m.focus = 2
	m.backupSel = "run-1"
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(model)
	if m.backupConfirm != nil {
		t.Fatal("running backup must not open delete confirm")
	}
	if !strings.Contains(m.backupNotice, "stop") {
		t.Fatalf("must require stop first: %q", m.backupNotice)
	}
	if cmd != nil {
		t.Fatal("blocked delete must not dispatch")
	}
}

func TestStopKeyShowsDisabled(t *testing.T) {
	m := fixtureForBackup(t)
	m.focus = 2
	m.backupSel = "run-1"
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(model)
	if m.backupConfirm != nil {
		t.Fatal("stop must not open confirm (no verified path)")
	}
	if !strings.Contains(m.backupNotice, "run-1") || !strings.Contains(m.backupNotice, "running") {
		t.Fatalf("stop notice must show name and phase: %q", m.backupNotice)
	}
	if cmd != nil {
		t.Fatal("stop must not dispatch")
	}
}

func TestConfirmKeysInactiveExceptPrompt(t *testing.T) {
	m := fixtureForBackup(t)
	m.focus = 2
	m.backupSel = "older"
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(model)
	// Tab/q must be inactive while confirming.
	for _, k := range []string{"tab", "q", "r", "n", "d"} {
		var key tea.KeyMsg
		if k == "tab" {
			key = tea.KeyMsg{Type: tea.KeyTab}
		} else {
			key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		// Normalize String() mapping: use Runes for letters.
		updated, cmd := m.Update(key)
		nm := updated.(model)
		if nm.focus != 2 || nm.backupConfirm == nil || cmd != nil {
			t.Fatalf("key %q must be inactive during confirm", k)
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.backupConfirm != nil {
		t.Fatal("esc must close confirm")
	}
}

func TestBackupViewStaysInViewport(t *testing.T) {
	m := fixtureForBackup(t)
	m.focus = 2
	m.width, m.height = 48, 20
	m.backupConfirm = &backupConfirm{kind: "delete", name: "older", phase: "completed"}
	m.backupNotice = "Delete failed: forbidden and some very long server message that must wrap or truncate safely"
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("viewport height changed: %d vs %d", len(lines), m.height)
	}
}
