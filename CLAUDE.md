# git-planner

Single-pane planner for GitHub issues and pull requests next to beads maps and
evidence reports. Go (stdlib only, no third-party modules) with an embedded
Svelte frontend. Run it against fictional data with `go run . --demo`.

## Build and test

```bash
go build ./...          # needs no Node: the frontend bundle is committed
go vet ./... && go test ./...
just gate-go            # build + vet + test + gofmt check
just frontend           # rebuild the Svelte bundle in web/ (needs Node + pnpm)
go run . --demo         # fictional data, needs git + bd
```

Some tests shell out to `git`, `bd` (beads) and `dot` (graphviz). `just gate`
rebuilds the bundle first; commit `internal/assets/static/app/` when it changes.

## Layout

- `main.go` — flags, listeners, demo mode switch
- `internal/server` — routes, MCP endpoint (`/mcp`), static assets
- `internal/appctx` — builds the tree the views render (selection, ordering and
  history live here, tested without a browser; `browser.js` only draws)
- `internal/ghrest` — GitHub REST client (`GKP_GITHUB_API` overrides the base URL)
- `internal/collectors` — git, beads (`bd`), GitHub GraphQL card
- `internal/reports` — append-only evidence store; `internal/bdops`,
  `internal/closeops`, `internal/taskops` — tracker writes
- `internal/demo` — fake GitHub API and demo workspace
- `web/` — Svelte sources; `docs/mcp-und-frontend.md` — MCP and payload reference

## Conventions

- Code and identifiers in English; the UI text and docs/ are partly German.
- Tokens come from the environment (`.env` is gitignored); never log or store values.
- `gofmt` decides formatting.
