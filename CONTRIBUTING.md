# Contributing to dqex

Thanks for your interest in dqex! We welcome contributions of all kinds — bug reports, feature requests, docs, tests, and code.

感谢对 dqex 的关注！我们欢迎各类贡献：Bug 反馈、功能建议、文档、测试与代码。

---

## Quick Start

```bash
# Prerequisites: Go 1.25+, Node 20+ (for web dev), make
make build          # build ./dqex binary
make test           # run all Go tests
make release        # cross-platform packages (5 platforms) into release/
```

Web frontend development (hot reload):

```bash
cd web
yarn install
yarn dev            # Vite dev server; backend still serves the embedded dist otherwise
```

## Project Layout

```
internal/cli      # CLI commands (cobra) — export/import/migrate/compare/snapshot/sql terminal...
internal/engine   # core engine — exporters, importers, migrators, SQL generation, snapshot
internal/service  # shared business logic used by both CLI and Web
internal/web      # Web API server (cygin), SSE streaming, token auth
internal/store    # local persistence (sqlite) + crypto
web/src           # React frontend (TypeScript, Vite, Tailwind)
```

Keep the layering: CLI/Web are thin adapters over `internal/service`, and the engine stays database-agnostic.

## Commit Message Style

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(web): add connection quick-switch from the object tree
fix(engine): escape single quotes when importing MySQL dumps
docs: update offline operations guide
chore(deps): bump infrakit to v1.0.1
```

Scope examples: `cli`, `web`, `engine`, `service`, `store`, `docs`, `deps`, `release`.

## Pull Request Process

1. Fork the repo and create a branch from `main` (`git checkout -b feat/xxx`).
2. Make your changes; keep them focused — one PR, one concern.
3. Run `go test ./...` and `make build` before pushing. Frontend-only changes: `cd web && yarn build`.
4. CI runs `go vet` + `go test ./...` on every PR — make sure it's green.
5. Open the PR against `main`, fill in what changed and why; reference related issues (e.g. `Closes #2`).

## Testing Guidelines

- New engine logic (exporters, importers, migrators, SQL generation) should come with table-driven tests — see `internal/engine/*_test.go` for existing patterns.
- Keep tests fast and offline: no real database required unless the test explicitly needs it.
- SQL generation tests use golden assertions per dialect (MySQL / PostgreSQL / Oracle).

## Good First Issues

Check the [`good first issue`](https://github.com/fj1981/dqex/labels/good%20first%20issue) label for beginner-friendly tasks.

## Code of Conduct

All participants must follow the [Code of Conduct](CODE_OF_CONDUCT.md). Be respectful, constructive, and patient — this project is maintained by volunteers.
