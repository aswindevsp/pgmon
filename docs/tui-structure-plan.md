# TUI structure implementation plan

## Goal

Give each dashboard tab a clear home while keeping the Bubble Tea application
model responsible for navigation, polling, and composing the screen. Preserve
the current UI and monitoring behavior during the move.

## Current layout

`internal/tui` contains the application model, discovery screen, rendering
helpers, and every dashboard view in one Go package. Some view code is already
in named files, but `dashboard.go` also renders the Overview and Pods tabs,
`app.go` contains view-specific summary lines, and `topology.go` mixes pod
rendering with navigation and scroll handling.

## Target layout

```text
internal/tui/
  app.go                  Bubble Tea model, messages, keys, polling
  dashboard.go            screen frame, tabs, viewport, view dispatch
  discovery.go            cluster picker
  navigation.go           pod selection and scroll handling
  run.go                  program entry point
  views/
    shared/               view input, panels, styles, formatting
    overview/             Overview tab
    replication/          Replication tab
    backups/              Backups tab and recovery display
    queries/              Queries tab
    locks/                Locks tab
    pods/                 Pods tab and topology rendering
```

Each directory is a Go package. The `tui` package imports the view packages;
view packages must not import `tui`. The `monitor` package remains responsible
for collection and data types.

## Package contract

- Define a small, read-only `shared.Context` for data needed by views: the
  `monitor.Snapshot`, selected pod, SQL freshness, query filter, sort choice,
  and detail state. Build it from the application model when rendering.
- Export one `Render(context, width)` function from each tab package. Keep
  tab-specific formatting helpers private to that package.
- Put panel layout, colors, and general formatting in `views/shared`. Do not
  put monitoring collection or navigation there.
- Keep cross-tab summaries explicit. For example, the Overview can call an
  exported backup summary function or receive prepared summary lines. Avoid
  importing one tab package from another unless the dependency has a clear
  owner and cannot form a cycle.
- Keep `model.Update`, refresh commands, tab selection, pod selection, and
  viewport scrolling in `internal/tui`.

## Migration steps

1. Move generic styles, panels, and formatting from `styles.go`, `format.go`,
   and `dashboard.go` into `views/shared`. Move `Run` out of `format.go` into
   `run.go`.
2. Introduce `shared.Context` and switch the screen body dispatcher to call
   tab renderers through it. Keep the screen frame in `dashboard.go`.
3. Move `queries_view.go` and `replication_view.go` into their tab packages.
   Split the Locks renderer from the Queries renderer; share only the session
   filtering they both need.
4. Move `backup_view.go` and `recovery_view.go` together into `views/backups`.
   Move the backup summary currently in `app.go` with them.
5. Move topology and pod detail rendering from `topology.go`, plus pod list
   and storage rendering from `dashboard.go`, into `views/pods`. Retain pod
   selection and scrolling in `navigation.go`.
6. Move the Overview renderer from `dashboard.go` into `views/overview`.
   Move its WAL and activity summaries from `app.go` or expose them as small
   helpers from their owning view packages.
7. Remove compatibility wrappers after callers and existing tests use the
   new package APIs. Update the README development layout description.

## Test placement

Keep Go `_test.go` files beside the package they test. Move view-specific
tests with their view packages and keep application navigation, refresh, and
discovery tests in `internal/tui`. A separate `test/` directory is appropriate
only if later integration tests exercise the built program or external
services across package boundaries.

## Completion criteria

- Each tab's rendering code and its view tests live in its own directory.
- `internal/tui` contains the application lifecycle, navigation, and screen
  composition, without tab-specific rendering logic.
- No view package imports `internal/tui`, and `internal/monitor` has no TUI
  dependency.
- The build and existing tests pass, and tab navigation, narrow layouts,
  stale-data display, backup progress, and recovery wording behave as before.
