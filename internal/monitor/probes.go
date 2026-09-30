package monitor

import _ "embed"

//go:embed probes/backup_probe.py
var backupProbe string

//go:embed probes/restore_probe.py
var restoreProbe string
