# Changelog

## [1.1.0] - 2026-08-25
### Added
- Workspace tab settings: configurable tab limit and pinned tabs
- Web service authentication strategy upgrade: loopback (127.0.0.1/localhost) access is auth-free, external sources require token, optional access whitelist, and `--no-auth` mode to fully disable authentication
- GitHub repository and contact info links in the About dialog

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
