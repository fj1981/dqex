# Changelog

## [1.8.0] - 2026-09-11
### Added
- Gitee mirroring: pushing to `main` or tagging `v*` now automatically syncs the code and all tags to the Gitee mirror (https://gitee.com/fjcn/dqex)
- Release announcements: every published release now automatically posts a version-announcement to the repository Discussions

### Changed
- README rewritten with a marketing focus, adding a Quick Start section and a comparison table against DBeaver / Navicat / DataGrip

## [1.7.4] - 2026-09-11
### Fixed
- The "About" dialog now shows the project homepage and contact info in open-source builds
- Underlying dependency library upgraded, restoring streaming-query cancellation

### Changed
- Sibling-repo sync script now excludes build-artifact directories and auto-maps cross-repo dependency paths

## [1.7.3] - 2026-09-09
### Added
- `ExportOptions.OnDetail` export detail callback: invoked synchronously as each table/object finishes exporting (`ExportDetail{Database, Kind, Name, Rows, Query, Ddl, Comment, Columns}` including table comment and column details), letting hosts build export manifests/audit trails as a side-channel — replaces post-hoc parsing of `.desc` files and SQL marker comments (the latter unavailable for gzip/zip artifacts and coupled to comment wording)
- Root package adds the `ExportDetail` type and `DetailKindTable/View/Function/Procedure` constants; `writeTableDDL` now returns the written DDL text
- Bare object-whitelist entries (`db._functions/name`) now match PG schema-qualified enum names (schema stripped as fallback, consistent with table filtering "matches same-named table in any schema"), so hosts can configure object whitelists with bare names across schemas
- FormatJSON data-package export emits table details as well (skip/no-PK tables with Rows=0, Ddl = written CREATE statement)

## [1.7.2] - 2026-09-08
### Fixed
- StartExport/StartDictionary re-emit one progress event after artifact relocation: the runner's terminal done event now carries the logical path (previously the tmp workspace path — deleted locally in object-store mode, so the host hook upload to the download list always failed)
- downloadUrl joined with the mount prefix via apiUrl(), fixing embedded-mode downloads hitting the host SPA fallback and saving artifacts as index.html

## [1.7.1] - 2026-09-08
### Added
- Exported `IsArtifactLogicalPath`: lets hosts recognize direct-upload artifact logical paths (as opposed to local paths, streamed from object storage)

## [1.7.0] - 2026-09-08
### Added
- Unified artifact accessor (artifact.go): logical paths `<prefix>/<name>`; object-store mode uploads in one step without landing a local final copy, standalone mode writes the managed DataDir; history/download/cleanup no longer care about the storage medium
- Compare reports (compares) fully wired to the unified accessor; OutputPath unified to logical paths
- Export package/dictionary single-file artifacts: object-store mode assembles into a tmp workspace, streams one upload, then cleans up; directory artifacts (Compress=false desktop scenario) keep local semantics
- Download endpoint splits by path shape: logical paths stream back, local paths keep FileAttachment; history deletion cleans up per shape
- Snapshot content upload/download/delete helpers folded into the accessor, behavior unchanged

## [1.6.0] - 2026-09-08
### Added
- Snapshot index moved into the database: from a single JSON file (O(n) read-modify-write, non-atomic on object storage and prone to losing the index) to the snapshot_index table (conn_id/created_at indexes + body_json entries); snapshot content remains per-id `{id}.json` in OSS/local, loaded on demand; StoreNone degradation keeps the JSON path
- Legacy indexes auto-migrate on first access: incremental idempotent upsert by ID, single-entry failures retry safely, legacy file kept read-only for rolling back to older versions
- GET /api/snapshots accepts an optional connId filter (virtual connections `env:<id>` isolated per environment); frontend connection filtering pushed down to the server

### Fixed
- Meta generic KV table column names avoid database reserved words (meta_key/meta_value)

## [1.5.1] - 2026-09-07
### Fixed
- go.sum synced with pk-infrakit-g v0.2.0 checksums

## [1.5.0] - 2026-09-04
### Changed
- All engine streaming queries switched to DirectForEachQueryContext (6 call sites across exporter/migrator/snapshot/compare/exporter_json): cancellation now closes the underlying connection immediately so the database actively terminates the SELECT — export/migrate cancel latency drops from "wait for the SQL to finish" to milliseconds (requires pk-infrakit-g v0.1.8 → v0.2.0)

## [1.4.1] - 2026-09-04
### Fixed
- AI replies strip reasoning-model `<think>` blocks (MiniMax M series adaptation): non-streaming stripThinkAll + a streaming state machine (handling tags split across incremental chunks), wired into Chat/ChatStream and the web main path Agent.Stream — typewriter output and persisted history stay clean

## [1.4.0] - 2026-09-04
### Added
- Multi-tenant data isolation: user-scoping mechanism isolates SQL history, favorites, audit, workspaces and AI sessions per user in embedded host scenarios; standalone deployments remain compatible via the default local scope
- External SQL storage WithStoreConn fully wired: cross-dialect metadata persistence into the host database
- AI assistant config injected via `WithAIConfig` (base_url/api_key/model owned by the host, never persisted), plus PG-family schema.table name support
- EmbedShell embeddable component: URL parameters inject connection and theme config for quick launch of task views
- `WithTaskHooks` task hooks: async task start/progress callbacks forwarded to the host (with initiator identity) so hosts can mirror task lists
- `Client.CancelTask`: forwards host-side task cancellation into a real engine interruption
- Snapshot object storage (WithArtifactStore): snapshot index + data land under the `snapshots/` prefix and survive container rebuilds; directory hot-reload skips the snapshots local migration
- Progress events enhanced: `Progress.Phase` stage marker (schema=DDL / data=row export) and `Progress.MsgSeq` event sequence — hosts distinguish "new event" from "snapshot refresh" by sequence number

### Changed
- Start* pre-resolve and normalize connections, avoiding failures when the connection changes between validation and execution
- Snapshot index read-modify-write distinguishes "object missing" from transient faults, preventing an empty list from overwriting an existing index on transient errors; snapshot indexing is lock-protected
- Embedded mode hides the desktop-only "open folder" and duplicate save buttons, conn pre-injection extends to export/dictionary/snapshot views, and a lang parameter follows the host language

### Fixed
- SQL database-name prefix trimming; sanitizeName checks emptiness before sanitizing to keep default-name semantics

## [1.3.0] - 2026-08-31
### Added
- Library mode (Go library) officially released: `dqexweb.Mount` allows host applications (e.g. tl-env) to mount the Web/API subtree with `MountOptions{Prefix, FrameAncestors, Fallback}` and embed the UI whole-page (`?embed=1#/embed/<view>`)
- `Client` execution/export/import engine and extension points: `RunSQLScript` / `RunExport` / `RunImport`, `WithConnProvider` / `WithConnHooks` / `WithQueryHooks` (write-operation auditing) / `WithContributors` (business-object fetch/write-back callbacks) / `WithDataPreparers`
- `RenderRowsSQL`: pure function rendering row data (map slices) into dialect-correct executable SQL text, reusing cydb dialect escaping (same source as the import/rollback pipeline) for host export-write shells, replacing hand-written dialect patches
- DataPackage data-format contract (JSON structure compatible with tl-env DataHolder, frozen): `ApplyDataPackage` import with precise rollback, transactional idempotent upsert, no-PK tables skipped with a warning

## [1.2.1] - 2026-08-27
### Changed
- Target database constraint checks are suspended during migration (session-level switch); databases with self-referencing foreign keys no longer fail on row ordering
- Migration tables are topologically sorted by foreign-key dependencies (referenced tables first)
- Upgraded infrakit to v1.2.0: batch writes now use multi-row VALUES (faster migration); progress page shows the current table name
- Table picker keeps loaded databases/tables and selection state across wizard steps; switching connections clears stale selections

### Fixed
- Workspace tabs blank after refresh/reopen, and empty snapshots overwriting saved workspace records (workspace was not restored when the connection was initialized from localStorage)
- Multi-database selection & migration experience: per-database table/object selections previously had mismatched database ownership and stale selections survived connection switches; selection is now per-database accurate (checking an unloaded database loads it and cascade-selects), and migration supports multiple source databases per task (target defaults to the source name and is created automatically)
- Export progress stuck near 0 in structure+data mode, and progress percentage regression during the object stage
- PG/Kingbase quoted qualified names (`"schema"."table"`) causing data read failures
- Migration progress never reaching 100% in DataOnly mode
- Table merging broken in the compare table-picking step (after the lazy-loading object tree refactor, target-only tables were no longer merged into their mapped source database nodes)

## [1.2.0] - 2026-08-26
### Added
- Object tree progressive loading: database list → schemas (with table counts) → objects, loaded level by level on click; unloaded nodes are grayed out (Navicat-style interaction)
- Metadata caching & refresh: 10-minute cache speeds up repeated expansion; tree-top refresh button and hover refresh on database/schema nodes bypass the cache to query the database directly
- SQL completion supports PG schema hierarchy: `db.` suggests schema names, `schema.` suggests tables/views
- Export table filter supports `db.schema.table` three-level qualified names (PG)

### Changed
- Metadata enumeration aggregated in the dialect layer: schema list + table counts in a single round-trip; system databases/users auto-filtered (MySQL system DBs, Oracle system users, PG/Kingbase system schemas)

### Fixed
- Kingbase connections still showing system schema `sys` in the object tree
- Upgraded infrakit to v1.1.0: fixed PG dialect `?` placeholder causing metadata/structure export failures

## [1.1.0] - 2026-08-25
### Added
- Workspace tab settings: configurable tab limit and pinned tabs
- Web service authentication strategy upgrade: loopback (127.0.0.1/localhost) access is auth-free, external sources require token, optional access whitelist, and `--no-auth` mode to fully disable authentication

### Changed
- Web service startup log optimized for loopback listeners: simplified auth prompt, emphasizing "local access is auth-free"

### Docs
- AI conversation demo GIF added to README

## [1.0.0] - 2026-08-21
### Added
- Official stable v1.0.0 release with multi-platform packages for macOS / Linux / Windows

## [0.6.0] - 2026-08-21
### Added
- Database dialect adapter layer extracted as a standalone module, providing a stable foundation for cross-dialect capabilities
- Complete project documentation in both Chinese and English

## [0.5.0] - 2026-08-21
### Added
- Port occupation detection: automatically checks port availability before Web service startup; supports terminating the occupying process and retrying binding, or opening the existing service directly; works on macOS/Linux/Windows
- `dqex stop` command: finds and terminates other running dqex processes

### Changed
- On macOS, reuses existing browser tab if the same URL is already open instead of opening a new one (supports Chrome / Safari / Firefox / Edge / Brave)
- Database types are now shown as brand-colored icons; connection dropdown upgraded to a two-line layout (name/short name and host:port on separate lines) with selected-item highlight and full address available on hover

### Fixed
- Connection selector content being fully truncated in narrow sidebars; name and address no longer squeeze each other after the layout hierarchy rework
- SQL syntax errors (Error 1064) when re-importing exported files: text columns (e.g. mediumtext storing JSON) containing special characters such as single quotes were not escaped per the target database dialect rules, aborting the import

## [0.4.0] - 2026-08-20
### Added
- "Changelog" section in About dialog with per-version accordion display

### Changed
- Bilingual (CN/EN) CLI command help texts

## [0.3.0] - 2026-08-19
### Added
- Upgraded AI assistant to ReAct Agent tool-calling mode
- SQL generation (GenSQL) with target-dialect DDL output
- CSV escape export for query results
- AI-assisted SQL features and configuration management
- Global configuration view and save APIs
- Compatible collation configuration option

### Changed
- Dark mode and help center with section navigation on Web
- Object tree filtering and table name display improvements

### Fixed
- AI panel scroll positioning and error retry issues
- Sensitive column filtering and duplicated messages in AI chat

## [0.2.0] - 2026-08-17
### Added
- Table data browsing: filter, sort, export and row-level CRUD
- SQL terminal: `\d+` index display and `\copy` meta-commands
- Snapshot management and snapshot comparison

## [0.1.0] - 2026-08-12
### Added
- Core database import / export / migration features
- Schema comparison with multi-database batch support
- Data dictionary export to Excel
- Web workbench UI and connection management
- CLI tooling with a full user manual
