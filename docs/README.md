# Static Mangal documentation

This fork is a Go 1.18 CLI inherited from the unmaintained upstream `metafates/mangal` project. The upstream README states that maintenance ended in April 2025; the inherited upstream identities were rehomed to this fork; the upstream warning remains historical context.

## Documents

| Document | Purpose |
| --- | --- |
| [Architecture](ARCHITECTURE.md) | Runtime boundaries, data flow, public interfaces, state, configuration, and package responsibilities. |
| [Codebase catalog](CODEBASE_CATALOG.md) | File- and package-level map of the application, commands, generated assets, build/release machinery, and test inventory. |
| [Code review](CODE_REVIEW.md) | Evidence-based findings, severity, affected paths, and recommended remediation order. |
| [Technical strategy](TECHNICAL_STRATEGY.md) | Product-oriented technical direction, target architecture, milestones, and decision gates. |
| [Maintenance guide](MAINTENANCE.md) | Local development prerequisites, validation, release ownership, operational boundaries, and documentation expectations. |

## Review scope and method

The review covers first-party Go source, tests, configuration, scripts, CI, release configuration, and existing Markdown. Vendored third-party code was not reviewed as application code. Findings are static: the local environment has no `go` executable, so compilation, tests, race detection, and live scraper checks were not run.

## Current assessment

The project has a useful, self-contained CLI product and clear functional seams: Cobra commands, a Bubble Tea TUI, scriptable JSON mode, source adapters, conversion formats, AniList metadata, and local history. The immediate blockers are not feature breadth:

1. **Fork identity is incomplete.** `go.mod`, imports, release targets, installer URLs, package targets, and user-facing docs remain upstream-owned.
2. **The download and TUI concurrency paths are unsafe.** Static review found data races, a nil-page panic path, and incorrect volume-path construction.
3. **Scraper delivery is an unverified remote-code path.** Lua scrapers are fetched from mutable GitHub configuration and executed with preloaded libraries, without a pinned manifest, signature, digest, or permission boundary.
4. **Maintenance automation is stale.** CI actions and toolchain versions are old; tests include live third-party endpoints and have little coverage of the highest-risk local workflows.

Treat this documentation as the baseline for a clean fork cutover and reliability program, not as a claim that every external source is currently operational.
