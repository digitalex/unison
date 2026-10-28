# AGENTS.md

Conventions for anyone — human or agent — working on the Unison codebase.

## Coordinating

Unison is built with Unison. If you were invited to a workspace, run
`unison --agent-help` and follow it: claim backlog items before starting,
narrate with `unison status`, ask in chat when unsure, close items with the PR
link, and park on `unison wait` between tasks.

## The isomorphism rule

Every capability must exist in all three surfaces with the same semantics and
granularity:

1. **Store** (`internal/store`): the operation, in one transaction, recording
   an event.
2. **API** (`internal/server`): one endpoint calling it.
3. **CLI** (`internal/cli`) *and* **web UI** (`web/src`): both calling that
   endpoint.

Add the row to the command table in `internal/guide/agent.md.tmpl` and the
usage text in `internal/cli/cli.go`. A change that adds a button without a
command (or the reverse) is incomplete.

Real-time updates come from events: any mutation that should appear live must
record an event in the store, and the UI refreshes the collection the event
type touches (`collectionFor` in `web/src/Workspace.jsx`).

## Scope

Phase 0 is deliberately minimal. Do not scaffold placeholder UI or stub
endpoints for deferred surfaces (Devbox, document library, custom
views). Build a surface when its backlog item is picked up. UI styling
is intentionally absent until the design is done on the isocan canvas.

## UI work

For UI work, follow [GUI.md](GUI.md) (the "GraphicalUIKomore" Graphical theme, with
references in `gui/`) and use the `graphical-ui`, `graphical-convert`, or
`graphical-audit` skill in `.agents/skills/` as appropriate.

## Workflow

* Branch (or `git worktree`) per task; one PR per backlog item.
* `go test ./...` must pass. Add tests for store and API behavior; the
  end-to-end CLI test in `internal/cli/cli_test.go` is the place for
  cross-surface flows.
* `gofmt` all Go code.
* If you touch `web/`, run `make web` and commit the rebuilt
  `internal/webui/dist` in the same PR.
* Never commit `.unison.json` (it holds your session token).
