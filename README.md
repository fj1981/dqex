<p align="center">
  <img src="docs/banner.png" alt="dqex — AI-Native Database Workbench" width="100%">
</p>

<div align="center">

# dqex

**The AI-Native, Offline-First Database Workbench**

[English](README.md) · [简体中文](README.zh-CN.md)

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.25-blue)](go.mod)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey)]()
[![Databases](https://img.shields.io/badge/databases-MySQL%20%7C%20PostgreSQL%20%7C%20Oracle-orange)]()
[![CI](https://github.com/fj1981/dqex/actions/workflows/ci.yml/badge.svg)](https://github.com/fj1981/dqex/actions/workflows/ci.yml)

[![Stars](https://img.shields.io/github/stars/fj1981/dqex?style=social&label=Star)](https://github.com/fj1981/dqex/stargazers)
[![Forks](https://img.shields.io/github/forks/fj1981/dqex?style=social&label=Fork)](https://github.com/fj1981/dqex/network/members)

</div>

---

## 🚀 One Tool. Every Environment. Even the Air-Gapped Ones.

dqex is a cross-platform database workbench that ships as a **single static binary** — no JVM, no Electron, no installers, no network required. It packs **export, import, migration, comparison, snapshots, Excel data dictionaries, a full SQL terminal, and an AI assistant** into one tool, with both a polished Web UI and a scriptable CLI.

- 🪶 **Zero-dependency** — copy it to a USB stick and run it on a bank's air-gapped server
- ⚡ **Starts in <1s**, ~50–60 MB memory footprint
- 🤖 **AI-native agent** — explores your *real* schema, writes dialect-correct SQL, and never executes without your confirmation
- 🧩 **Web + CLI, one engine** — same connections, saved tasks, and history on both sides
- 🌐 **MySQL · PostgreSQL · Oracle** with automatic dialect conversion for cross-type migration
- 📋 **Excel data dictionaries & snapshot diff reports** — compliance-ready by design (等保 / GDPR / audits)
- 🏠 **Take production data home safely** — conditional export + gzip, restore to your local test env in 2 commands

---

## ✨ Feature Highlights
y
| Feature | Web | CLI | Description |
|---|---|---|---|
| Export | ✅ | `export` (`exp`) | Schema + data → SQL file (zip / gzip), conditional & consistent |
| Import | ✅ | `import` (`imp`) | SQL / zip file → database, auto create tables, batch inserts |
| Migrate | ✅ | `migrate` (`mig`) | Database → database, **cross-dialect** (e.g. MySQL → PostgreSQL) |
| Compare | ✅ | `compare` (`cmp`) | Schema & data diff between two databases |
| Data Dictionary | ✅ | `dictionary` (`dict`) | Tables + comments → styled Excel (.xlsx), audit-ready |
| Snapshot | ✅ | `snapshot` (`snap`) | Full create / list / show / delete / compare lifecycle |
| SQL Query | ✅ | `sql` | Web query terminal + table browser; CLI interactive REPL with JSON output |
| AI-Assisted SQL | ✅ | `\ai` in `sql` | Agent probes real schema, generates SQL per dialect, requires confirmation |
| Connections | ✅ | `conn` (`cn`) | Save / test / delete database connections |
| Saved Tasks | ✅ | `task` (`tk`) | Reuse one-click task configs (`--task <ID>`) |
| History | ✅ | `history` (`his`) | Execution log for troubleshooting & audit export |
| Shell Completion | — | `completion` | zsh / bash completion scripts |

---

## 🧑‍💻 Quick Start

### Download

Grab the zip for your platform from the [Releases page](https://github.com/fj1981/dqex/releases) and unzip — **no installation required**.

### Linux / macOS

```bash
./install.sh                    # install to /usr/local/bin (optional)
dqex                            # start Web UI at 127.0.0.1:8181
# or run without installing:
./start.sh                      # foreground (Ctrl+C to stop)
./start.sh -d                   # background daemon
./stop.sh                       # stop the daemon
```

### Windows

```bat
install.bat                     :: install to %LOCALAPPDATA%\dqex and add to PATH
dqex                            :: start Web UI in a new terminal
:: or run without installing:
start.bat                       :: foreground
start.bat -d                    :: background
stop.bat                        :: stop background service
```

### CLI in 30 seconds

```bash
# Save a connection once, reuse everywhere
dqex conn add --name prod --type mysql --host 10.20.16.170 --port 3317 --un root --pw 'xxx'

# Export with conditions & gzip → take it home
dqex exp camunda -s prod -o backup.sql.gz --table-cond "orders:created_at >= '2026-01-01'"

# Restore to your local test database
dqex imp -t local_test -i backup.sql.gz --reset drop-and-create

# Interactive SQL terminal (native dialect, runs on the target DB)
dqex sql -c prod
dqex sql -c prod --json "SELECT id, name FROM users LIMIT 10"   # agent-friendly JSON

# Snapshot & compare — the killer feature for incident tracing
dqex snapshot create -c prod -n baseline
dqex snapshot compare -c prod --a baseline --b after-deploy

# Compliance: one command → styled Excel data dictionary
dqex dict camunda -s prod -o data_dict.xlsx
```

---

## 🤖 AI-Assisted SQL (Optional, Offline-Safe)

- **Real schema, not guesses** — the agent queries your actual table structures before generating SQL (Web UI shows live progress)
- **Generate ≠ Execute** — AI only produces SQL text; write operations require confirmation, dangerous statements are blocked, and you can `\e`-edit before running
- **Full assist loop** — `\ai continue` for follow-ups; Web UI supports generate / explain / optimize / fix with side-by-side diff preview and one-click apply
- **Keys stay local** — API key stored on your machine; only masked endpoint & model name are shown
- **No config, no footprint** — the AI entry only appears after BaseURL / API Key / Model are all configured; everything else keeps working untouched
- **Offline fallback** — built-in SQL template library (`\template top_n orders amount 10`) works in fully isolated networks

---

## 🛠️ Build from Source

```bash
make dev                # Go :8181 + Vite :5281 with hot reload & debugging
make build              # single binary ./dqex (frontend embedded)
make release            # cross-platform packages → release/
make install            # → /usr/local/bin
```

---

## 🧱 Tech Stack

- **Backend**: Go + [infrakit](https://github.com/fj1981/infrakit) (database dialect adaptation), Gin, embedded SQLite
- **Frontend**: React + TypeScript + Vite + Tailwind CSS + shadcn/ui + Monaco Editor
- **Excel**: excelize (pure Go, no CGO)
- **Distribution**: single static binary with embedded frontend, dark mode, i18n (EN/中文)

---

## 📚 Documentation

- [CLI Manual](CLI.md) — every command, flag, and meta-command
- [Project Overview](docs/OVERVIEW.md) — philosophy, personas, and deep-dive scenarios
- [Engineering Conventions](docs/conventions.md) — state modeling & data-flow rules (read before contributing)

---

## 🤝 Contributing

Contributions are what make open source great — and they earn you a place on the stargazers list too! ⭐

We welcome:
- 🐛 Bug fixes (highest priority)
- 📝 Documentation improvements (EN / 中文)
- 🧩 New SQL templates for the offline library
- 🗄️ Additional database driver support
- 🎨 UI/UX polish

**How to contribute:**

1. Fork the repo
2. Create a branch: `git checkout -b feature/your-feature`
3. Commit your changes: `git commit -am 'feat: add something awesome'`
4. Push: `git push origin feature/your-feature`
5. Open a Pull Request

Conventions: Go code follows `gofmt`; TypeScript follows ESLint + Prettier; commits follow [Conventional Commits](https://www.conventionalcommits.org/).

---

## 📄 License

[MIT](LICENSE) © 2026 fj1981

**⭐ Star us on GitHub** — it tells us you care, and helps more people discover the tool. Fork it, play with it, break it, and send us a PR!
