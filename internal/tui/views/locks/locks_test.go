package locks

import (
	"strings"
	"testing"

	"postgres-monitor/internal/monitor"
	"postgres-monitor/internal/tui/views/shared"
)

func lockFixture() shared.Context {
	ctx := shared.Context{SQLLive: true}
	ctx.Data.Sql.Sessions = []monitor.SessionStatus{{PID: 10, State: "idle in transaction", Query: "UPDATE shop SET quantity=1"}, {PID: 20, State: "active", Query: "SELECT * FROM shop", BlockingPIDs: []int{10}}}
	return ctx
}

func TestBlockingRelationship(t *testing.T) {
	chain := strings.Join(BlockingLines(lockFixture()), "\n")
	if !strings.Contains(chain, "pid 10") || !strings.Contains(chain, "pid 20") {
		t.Fatal("blocking relationship missing")
	}
}

func TestLocksShowWaitingSessions(t *testing.T) {
	view := Render(lockFixture(), 118)
	if !strings.Contains(view, "pg_blocking_pids") || !strings.Contains(view, "pid 20") {
		t.Fatal("waiting session missing:", view)
	}
}

func TestLocksHideStaleData(t *testing.T) {
	ctx := lockFixture()
	ctx.SQLLive = false
	if strings.Contains(Render(ctx, 118), "pid 10") {
		t.Fatal("stale data presented as current")
	}
}
