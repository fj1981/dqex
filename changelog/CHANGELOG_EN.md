# Changelog

## [0.5.0] - 2026-08-21
### Added
- Port occupation detection: automatically checks port availability before Web service startup; interactively prompts to terminate occupying process and retries binding; supports macOS/Linux/Windows
- `dqex stop` command: finds and terminates other running dqex processes

### Changed
- Binary renamed from `dbx` to `dqex` (d=database, q=query, e=execute, ex=export/extension); all CLI commands, scripts, and docs updated accordingly
- Config directory renamed from `~/.dbimpex` to `~/.dqex`; environment variable renamed from `DBIMPEX_CONFIG` to `DQEX_CONFIG`

## [0.4.0] - 2026-08-20
### Added
- "Changelog" section in About dialog with per-version accordion display

### Changed
- Bilingual (CN/EN) CLI command help texts

## [0.3.0] - 2026-08-19
### Added
- Upgraded AI assistant to React Agent tool-calling mode
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
