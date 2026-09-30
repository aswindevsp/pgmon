# pgwatch

A read-only terminal monitoring dashboard for CloudNativePG clusters. It uses your existing
`kubectl` context and authentication, then runs a fixed PostgreSQL monitoring
query in the selected primary pod.

## Run

```sh
go run .
```

By default, pgwatch uses your current `kubectl` context and discovers CNPG
clusters across all namespaces. If there is one cluster, it opens automatically.
If there are multiple clusters, use ↑/↓ or Tab to select one and Enter to open it.
The selector includes namespaces so clusters with the same name are distinguishable.
If discovery fails or finds no clusters, press `r` to retry or `q` to quit.

Use `-context` to choose another context, `-namespace` to limit discovery, or
`-cluster` to filter by cluster name. Supplying both a namespace and cluster name
opens that cluster directly without requiring permission to list all clusters:

```sh
go run . -namespace my-namespace -cluster my-cluster
```

Discovery across namespaces requires cluster list access. If your access is
limited to a namespace, use `-namespace`. The chosen context is fixed for the
session, even if you change your current kubectl context in another terminal.

## Dashboard

The reference-style dark terminal dashboard has six tabs:

1. **Overview** — compact topology, connections, TPS, cache statistics, backup summary,
   alerts, and Kubernetes events.
2. **Replication** — replica initialization with live physical base copy progress
   and measured transfer speed, sent/write/flush/replay WAL positions, byte backlog,
   recent backlog trends, and replication slot retention.
3. **Backups** — running and queued backups, process-read progress estimates, history,
   reported recovery point, schedules, and WAL archiver statistics.
4. **Queries** — top statements when `pg_stat_statements` is already available,
   session activity, wait events, and blocking relationships.
5. **Locks** — blocking relationships and sessions waiting on locks.
6. **Pods** — readiness, restarts, node placement, replication details, and PVC capacity.

Panels stack in narrow terminals. Stale PostgreSQL metrics are hidden from live
panels; collection failures remain visible. CPU, memory and filesystem usage are
shown as unavailable because those metrics are not collected. Restore checks and
WAL continuity are not inferred from successful backups.

| Key | Action |
| --- | --- |
| `1`–`6` / Tab / Shift+Tab | Switch tabs |
| `j` / `k` or Up / Down | Move rows; select a pod on Pods |
| Enter | Show collected query text; open Pods from Overview |
| `/` | Filter queries and sessions; Enter applies |
| `s` | Sort returned top statements by calls or total execution time |
| Escape | Clear filter and close details/help |
| Page Up / Page Down / Space | Scroll |
| Home / End | Jump to top / bottom |
| `?` | Toggle help |
| `r` | Refresh |
| `q` / Ctrl+C | Quit |

The screenshot actions that change state are intentionally absent: the app cannot
start/cancel backups, restore databases, drop slots, cancel queries, terminate
sessions, or change cluster settings. It does not execute EXPLAIN on observed SQL.

## Development

```sh
go test ./...
go build .
```

The dashboard is a compiled Bubble Tea TUI, so the terminal cannot swap in new
code the way a web app hot-reloads a module. The practical workflow is **rebuild
and restart** on save. State such as the selected tab, filter, and scroll
position is lost on each restart.

`gow` watches `.go` files, keeps a real TTY so keyboard input still works, and
restarts `go run` after each successful change:

```sh
go install github.com/mitranim/gow@latest
make dev
make dev ARGS="-namespace my-namespace -cluster my-cluster"
```

Or run `gow` directly:

```sh
gow run .
gow run . -namespace my-namespace -cluster my-cluster
```

Do not use `air` for this app. It does not attach the child process to your
terminal, so the TUI cannot read keys or draw the alternate screen correctly.

The UI tests use local fixtures and do not access Kubernetes.

The root `main.go` handles flags and process exit. `internal/tui` owns the
application lifecycle, polling, discovery, tab selection, pod selection, and
screen composition. Each dashboard tab renders from its own package under
`internal/tui/views` (`overview`, `replication`, `backups`, `queries`, `locks`,
and `pods`), with shared panels, styles, and formatting in
`internal/tui/views/shared`. `internal/monitor` owns Kubernetes discovery,
read-only SQL, collection, bounded histories, and embedded probes. The monitor
package has no terminal UI dependencies, and no view package imports
`internal/tui`.

## Access and query behavior

The app reads `kubectl config current-context`, uses `kubectl get` to discover
and monitor resources, and uses `kubectl exec` for PostgreSQL reads. The
SQL is fixed in the program and runs inside `BEGIN READ ONLY` with a 3 second
statement timeout. It reads CloudNativePG cluster status, pods, PVCs, backups,
scheduled backups, events, and PostgreSQL replication, archiver, WAL, and
activity statistics. The app does not expose shell or SQL input.

Cluster status and PostgreSQL metrics refresh every five seconds. The other
Kubernetes resources refresh every fifteen seconds. PVC capacity is shown as
provisioned capacity; this first version does not collect filesystem usage.
Replica initialization progress comes from the primary's
`pg_stat_progress_basebackup` view. Copy speed is the byte difference between
two successful samples from the same process divided by elapsed time; it appears
after the second sample and resets when the join process changes.

Optional statement statistics are read only from an existing `pg_stat_statements`
view in the `postgres` database. Missing extensions or permissions are reported;
the app never installs extensions or changes PostgreSQL settings. It collects up
to 15 statements and 200 sessions, with query text capped at 4,096 characters.
Statement percentages refer to the returned top statements. TPS needs two valid
samples from the same primary; cumulative database counters cover the period since
their statistics reset. Replication histories are bounded to 60 successful samples.

## Live backup progress

Press **3** to open Backups. Active backups take priority over newer queued
backups. The panel includes manually created backups, filters by `spec.cluster.name`,
and shows pending backup count, elapsed time, and the backup pod.

For Barman Cloud, the monitor samples the matching parent backup process every
Kubernetes refresh (15 seconds, plus collection time). It supports the native
`plugin-barman-cloud` sidecar and the older built-in `postgres` container. It
shows process bytes read, read throughput, CPU cores used, and an estimated
progress bar using PostgreSQL database size. A second sample is needed for rates;
process restarts reset the rate baseline. Missing Python, inaccessible process
counters, or denied exec access show an unavailable message. Old samples are
marked stale.

**The percentage and remaining read time are estimates, not upload progress.**
Process reads can include overhead, databases can grow, and upload/finalization
can continue after reading finishes. Estimates stay below 100% until CNPG reports
`completed`. Azure committed/uncommitted block counts are not collected by this
view. No storage credentials are read. The monitor never starts, cancels, or
changes a backup. It uses fixed read-only Python and SQL probes through `kubectl
exec`; the sidecar needs Python 3 and readable Linux `/proc` counters.
