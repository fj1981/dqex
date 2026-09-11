# Comparison: dqex vs DBeaver, Navicat, DataGrip and Bytebase

> Last reviewed: 2026-09 · dqex v1.2.1
>
> Competitor capabilities change. If something here is out of date, please
> [open an issue](https://github.com/fj1981/dqex/issues) — corrections are welcome and will be merged.

---

## The short version

**If DBeaver works in your environment, use DBeaver.** It is a bigger, more mature tool, it is free, and it supports far more database engines than dqex does. This document is not an argument that dqex is better in general — it is a description of a specific gap.

dqex is built for two situations:

1. **You cannot install things.** An air-gapped network, a locked-down production jump host, a bank's internal server, a client's machine you have no admin rights on. Shipping a JVM plus a pile of driver jars by hand is a project in itself, and it is a project you have to repeat on every machine. dqex is one file.
2. **Your deliverable is a document, not a session.** Audits and 等保 reviews do not ask for a screenshot of your SQL client. They ask for a data dictionary, a schema diff, an execution log. Those are one command in dqex and a plugin hunt everywhere else.

Everything else is secondary.

---

## Feature matrix

| | dqex | DBeaver | Navicat | DataGrip | Bytebase |
|---|---|---|---|---|---|
| **License** | MIT | Apache-2.0 (CE) | Commercial | Commercial | Mixed (open core) |
| **Price** | Free | Free (CE) / paid (PRO) | Paid per seat | Paid | Free / paid tiers |
| **Install form** | Single static binary, frontend embedded | JVM + driver management | Native installer | JetBrains IDE | Server-side service |
| **Runtime dependency** | None | Java runtime | Native | JVM | Server + database |
| **CGO required** | No | N/A | N/A | N/A | N/A |
| **Typical footprint** | ~50–60 MB RAM | Several hundred MB | Several hundred MB | Several hundred MB | Depends on deployment |
| **Cold start** | Under 1s | Seconds | Seconds | Seconds | Page load |
| **Air-gapped deployment** | Copy a file and run | Manual JVM + jar shipping | License-constrained; offline activation needed | License-constrained | Needs a server, usually a change request |
| **Works with no admin rights** | Yes | Usually | Usually | Usually | No (server) |
| **GUI** | Web UI (local HTTP) | Desktop (SWT) | Desktop | Desktop | Web |
| **CLI** | Full parity, JSON output | Limited | Limited | No | API / CI |
| **Automation / scripting** | Every command, `--json` | Limited | Limited | No | Yes, CI-centric |
| **Cross-dialect migration** | Built in (MySQL→PostgreSQL→Oracle) | No | Yes | Partial | Via migration workflow |
| **Schema + data diff** | Built in | Schema compare (PRO) | Yes | Partial | Yes (schema) |
| **Snapshot & diff over time** | Built in | No | No | No | Migration history |
| **Excel data dictionary** | One command, styled `.xlsx` | Community plugin | Yes | No | No |
| **Conditional export** | Per-table conditions + gzip | Partial | Yes | Limited | No |
| **AI-assisted SQL** | Built in, real-schema grounded, confirmation required, offline fallback | No | Limited | Limited | Limited |
| **AI works fully offline** | Yes (template library) | N/A | No | No | No |
| **Telemetry / account required** | None | Optional | Activation | Activation | Account |
| **Database engines** | MySQL, PostgreSQL, Oracle | Very wide (100+ via JDBC) | Wide | Wide | MySQL, PostgreSQL, others |
| **Plugin ecosystem** | None yet | Large | Medium | Large | Medium |
| **Maturity** | New (2026) | Very mature | Very mature | Mature | Mature |

---

## Dimension by dimension

### Installation and deployment

This is the entire point of dqex. One file, `chmod +x`, run. The React frontend is embedded in the Go binary, so there is no separate asset directory, no Node runtime, and no install step. Copying it to a USB stick is a legitimate deployment method, not a workaround.

DBeaver and DataGrip are JVM applications. In a normal environment that is invisible. In a restricted environment it means: find a JRE of a compatible version, get it onto the machine, get the driver jars for your database onto the machine, keep them in sync, and repeat for every machine and every upgrade. Navicat is a native binary but its licensing model assumes it can reach an activation server, which is exactly what an air-gapped network cannot do.

Bytebase is a different shape entirely — it is a server-side change management platform, not a desktop client. It is a great fit for teams that want to gate schema changes through a review process, and a poor fit if you just need to export a table on a jump host.

### Getting to first query

dqex: unzip, run, open `127.0.0.1:8181`, add a connection. Under a minute, and nothing to configure beforehand.

The others require installing or activating first. That is not a criticism — it is the cost of being a much larger application. It just means the "I need to look at one table right now" case is slower.

### Cross-dialect migration

`dqex migrate` moves a database into a different engine and converts the schema. MySQL → PostgreSQL is the common case. Types, auto-increment semantics, quoting and default values are translated by the dialect adaptation layer. DBeaver has no equivalent; Navicat does this well and is more mature at it.

Note honestly: automatic cross-dialect conversion cannot be perfect. Vendor-specific types, stored procedures and collation behaviour have no clean equivalent, and dqex will report what it could not translate rather than silently dropping it. Always review the generated DDL before running it against anything important.

### Compliance artifacts

`dqex dict` produces a styled `.xlsx` with tables, columns, types, nullability and comments — the thing an auditor actually asks for. `dqex snapshot` records a schema state and diffs any two of them, which is the fastest way to answer "what did this deploy change?". `dqex history` gives an exportable execution log.

In the other tools this is either a plugin, a PRO feature, or simply not available. DBeaver can compare schemas in PRO; it has no snapshot-over-time concept and no data dictionary export.

### AI-assisted SQL

The differences that matter are not "does it have AI" but:

- **Does it read your real schema first?** dqex calls read-only tools (`list_tables`, `get_schema`) against the live database, then generates SQL from actual table structure. A model guessing your column names produces plausible-looking SQL that fails.
- **Can it run writes on its own?** In dqex, no. AI produces SQL text only; writes need explicit confirmation and dangerous statements are blocked. You can edit with `\e` before executing.
- **What happens with no network?** dqex's AI is opt-in and inert until configured — with nothing configured, the entry point does not render. In a fully isolated network, the built-in SQL template library still works.

Navicat and DataGrip both have AI assistance. Neither is designed around an offline-first, confirmation-required model, and neither is intended to run in an air-gapped network.

### Database breadth

This is dqex's biggest weakness, stated plainly: **three engines.** MySQL, PostgreSQL and Oracle. DBeaver supports over a hundred via JDBC and handles esoteric types and vendor quirks that dqex has not seen. If your work involves SQL Server, ClickHouse, MongoDB, Snowflake, or anything else, DBeaver or DataGrip is the right tool today.

Adding drivers is a welcome contribution — see [CONTRIBUTING.md](../CONTRIBUTING.md).

---

## Choosing

| Your situation | Use |
|---|---|
| Air-gapped / restricted network, no admin rights | **dqex** |
| You need a data dictionary or schema diff as an audit deliverable | **dqex** |
| You want to script database tasks and consume JSON | **dqex** |
| You need one tool for MySQL, PostgreSQL and Oracle | **dqex** or Navicat |
| You need SQL Server, ClickHouse, MongoDB or 20 other engines | **DBeaver** |
| You want a large plugin ecosystem and years of edge-case handling | **DBeaver** |
| You want a polished desktop client and will pay for it | **Navicat** |
| You live inside JetBrains IDEs | **DataGrip** |
| You want team-level schema change review and CI gating | **Bytebase** |

These are not mutually exclusive. A reasonable setup is dqex on the restricted machines and DBeaver on your laptop.

---

## What dqex is not

Being explicit about this saves everyone time:

- **Not a replacement for DBeaver.** It does not have the engine coverage, the plugin ecosystem, or the years of accumulated edge-case fixes.
- **Not a schema migration platform.** It moves and compares databases; it does not implement team review workflows, approval gates or version-controlled migrations. That is Bytebase's job.
- **Not a GUI-only tool, and not a CLI-only tool.** Both exist and share the engine, which is unusual but means neither is an afterthought.
- **Not a managed service.** There is no cloud offering and no account. You run the binary.
- **Not audited for correctness by anyone but its users.** Test the migration output before trusting it. Issues are the fastest way to get a fix.

---

## Method

This comparison was assembled from public documentation and hands-on use of each tool. Where a claim about a competitor could not be verified, it was left qualitative rather than asserted precisely. Feature availability often differs between free and paid tiers, and vendors change their plans — treat the competitor columns as a starting point for your own evaluation, not as a specification.

Corrections are welcome and will be merged. If you maintain one of these tools and disagree with something here, please
[open an issue](https://github.com/fj1981/dqex/issues) and it will be fixed.
