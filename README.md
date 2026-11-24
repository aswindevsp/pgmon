# pgmon

A terminal monitoring dashboard for CloudNativePG clusters with backup
actions in the Backups tab and primary switchover on Overview and Pods.
It uses your existing `kubectl` context and authentication, then runs a
fixed PostgreSQL monitoring query in the selected primary pod.

## Run

```sh
go run .
```

By default, pgmon uses your current `kubectl` context and discovers CNPG
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

1. **Overview** — compact instance cards (role, readiness, CPU/memory, replica
   lag), connection/TPS/cache load, WAL and replica vitals, ranked attention,
   and a single-line backup status.
2. **Replication** — replica initialization with live physical base copy progress
   and measured transfer speed, sent/write/flush/replay WAL positions, byte backlog,
   recent backlog trends, and replication slot retention.
3. **Backups** — running and queued backups, which instance pod ran each backup,
   process-read progress estimates, the last five backups, reported recovery
   point, schedules, and WAL archiver statistics.
4. **Queries** — top statements when `pg_stat_statements` is already available,
   session activity, wait events, and blocking relationships.
5. **Locks** — blocking relationships and sessions waiting on locks.
6. **Pods** — readiness, restarts, node placement, replication details, PVC
   capacity, and replica promotion (switchover).

Panels stack in narrow terminals. Stale PostgreSQL metrics are hidden from live
panels; collection failures remain visible. Instance CPU and memory come from
metrics.k8s.io; filesystem usage is still not collected, so only PVC capacity is
shown. Restore checks and WAL continuity are not inferred from successful backups.

| Key | Action |
| --- | --- |
| `1`–`6` / Tab / Shift+Tab | Switch tabs |
| `j` / `k` or Up / Down | Move rows; select an instance on Overview/Pods; select a backup on Backups |
| Enter | Show collected query text; open Pods from Overview |
| `/` | Filter queries and sessions; Enter applies |
| `s` | Sort returned top statements by calls or total execution time |
| Escape | Clear filter and close details/help/prompt |
| Page Up / Page Down / Space | Scroll |
| Home / End | Jump to top / bottom |
| `?` | Toggle help |
| `r` | Refresh |
| `q` / Ctrl+C | Quit |
| Overview / Pods `p` | Switchover: promote the selected replica with `kubectl cnpg promote` |
| Backups tab `n` | Start backup (confirm method and cluster) |
| Backups tab `x` | Stop backup (unavailable: no verified cancellation path) |
| Backups tab `d` | Delete Backup resource (terminal backups only, confirm exact name) |
| Confirm prompt `y` / Enter | Confirm backup or switchover action |
| Confirm prompt Escape | Cancel without acting |

The app cannot restore databases, drop slots, cancel queries, or terminate
sessions. It does not execute EXPLAIN on observed SQL. Switchover is the
only cluster topology change: it promotes a selected replica and never
edits other cluster spec fields.

Stop is disabled for every backup method: no verified cancellation path has
been established, deleting a `Backup` object does not stop its worker, and the
app never kills PostgreSQL pods or processes as a substitute.

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
and monitor resources, and uses `kubectl exec` for PostgreSQL reads. Exec
access on instance pods (already required for SQL on the primary) is also used
on a starting replica. The SQL is fixed in the program and runs inside
`BEGIN READ ONLY` with a 3 second statement timeout. It reads CloudNativePG
cluster status, pods, PVCs, backups, scheduled backups, events, and PostgreSQL
replication, archiver, WAL, and activity statistics. The app does not expose
shell or SQL input.

Backup actions use `kubectl create -f -` for one `Backup` resource and
`kubectl delete backups.postgresql.cnpg.io <name>` for removal, always in the
session's fixed context and selected namespace. They need `create` and `delete`
on `backups.postgresql.cnpg.io`; a verified stop mechanism would need its own
permission, but stop is currently disabled. The app never alters
`ScheduledBackup` resources.

Switchover uses `kubectl cnpg promote <cluster> <instance>` in the same fixed
context and namespace, as documented by the CloudNativePG kubectl plugin.
The `cnpg` plugin must be installed and able to update the Cluster. Select a
ready replica (not the current primary), confirm the current → target pair,
then watch `currentPrimary` update. A switchover already in progress, a
recovering cluster, stale Kubernetes data, or an unready target is rejected.
The request is re-checked against a fresh Cluster and Pod read before promote
runs. A successful plugin call is not the same as a completed switchover;
the dashboard waits until `currentPrimary` matches the target.

Cluster status and PostgreSQL metrics refresh every five seconds. The other
Kubernetes resources refresh every fifteen seconds. PVC capacity is shown as
provisioned capacity; this first version does not collect filesystem usage.
Replica initialization progress comes from the primary's
`pg_stat_progress_basebackup` view. Copy speed is the byte difference between
two successful samples from the same process divided by elapsed time; it appears
after the second sample and resets when the join process changes. After the base
copy, while the new instance replays WAL from the archive before its first
consistent recovery state, the primary cannot observe it. Through read-only
`kubectl exec`, the monitor samples the starting replica every five seconds:
the control file's minimum recovery ending location is the live flush cursor,
anchored by the restartpoint location, and the backup manifest fixes the
consistency target (the backup end). `pg_wal` is not used: CNPG's parallel
restore keeps prefetched segments there ahead of replay, so its newest file
would badly overstate progress. The dashboard shows the live position,
remaining bytes, a measured rate, and an ETA derived from it. The rate is
carried across container restarts and dropped after about 20 minutes without
movement. If the live cursor has not moved for over five minutes, the summary
says so, distinguishing a stalled archive fetch from slow replay.

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
and shows pending backup count, elapsed time, and which instance pod ran
or is running each backup (`status.instanceID.podName`).

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
view. No storage credentials are read. The monitor uses fixed read-only Python
and SQL probes through `kubectl exec`; the sidecar needs Python 3 and readable
Linux `/proc` counters.

Press `n` in Backups to start an on-demand `Backup` for the displayed context,
namespace, cluster, and configured method (`plugin` with
`barman-cloud.cloudnative-pg.io`, `barmanObjectStore`, or `volumeSnapshot`).
Confirm the method and cluster, then watch the created resource name appear in
the running panel and the last-five history list. Only CNPG phase `completed` confirms success;
a successful create request does not. Select with Up/Down, press `d` to delete
only a terminal backup after confirming its exact name, and `x` to see why stop
is unavailable. Delete removes only the Kubernetes `Backup` resource and never
promises object-store, WAL, or snapshot cleanup. A running backup cannot be
deleted until it is observed as no longer active, and every stop/delete
re-reads the resource to check UID, cluster, and phase.

## Primary switchover

Press **1** or **6** to open Overview or Pods. Use Up/Down to select a replica,
then `p` to request a graceful switchover. Confirm the current primary and
target instance. The monitor runs
`kubectl cnpg promote <cluster> <instance>`
([CNPG kubectl plugin](https://cloudnative-pg.io/documentation/1.20/kubectl-plugin/))
and keeps the notice until the Cluster reports the new `currentPrimary`.
Promote of the current primary, a Job pod, or an unready replica is blocked.
Install the plugin (`kubectl krew install cnpg` or the project install script)
before using this action.
