package queries

import (
	"strings"
	"testing"

	"pgmon/internal/monitor"
	"pgmon/internal/tui/views/shared"
)

func queryFixture() shared.Context {
	ctx := shared.Context{SQLLive: true}
	ctx.Data.Sql.TopQueries = []monitor.QueryStat{{Query: "SELECT orders FROM shop", Calls: 1, TotalExecTime: 100}, {Query: "SELECT payments FROM billing", Calls: 10, TotalExecTime: 5}}
	ctx.Data.Sql.Sessions = []monitor.SessionStatus{{PID: 10, State: "idle in transaction", Query: "UPDATE shop SET quantity=1"}, {PID: 20, State: "active", Query: "SELECT * FROM shop", BlockingPIDs: []int{10}}}
	return ctx
}

func TestFilteredQueriesSortOrder(t *testing.T) {
	ctx := queryFixture()
	if filteredQueries(ctx)[0].Calls != 1 {
		t.Fatal("default order should use total execution time")
	}
	ctx.SortCalls = true
	if filteredQueries(ctx)[0].Calls != 10 {
		t.Fatal("calls sort not applied")
	}
}

func TestQueryFilterAndDetails(t *testing.T) {
	ctx := queryFixture()
	ctx.Filter = "orders"
	if len(filteredQueries(ctx)) != 1 {
		t.Fatal("filter not applied")
	}
	ctx.Detail = true
	if !strings.Contains(Render(ctx, 118), "SELECT orders FROM shop") {
		t.Fatal("query details missing")
	}
}

func TestQueriesHideStaleData(t *testing.T) {
	ctx := queryFixture()
	ctx.SQLLive = false
	if strings.Contains(Render(ctx, 118), "SELECT orders") {
		t.Fatal("stale data presented as current")
	}
}
