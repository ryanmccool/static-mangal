# Maintenance guide

## Current validation status

This repository could not be compiled or tested in the review environment because the `go` executable is unavailable (`go list ./...` returned command-not-found). No claim in the review documents implies that external providers, builds, or tests currently pass.

Before making or accepting a behavioral change, install the Go version selected by this fork and run the smallest relevant check first. For changes to downloader, TUI, provider, history, networking, or filesystem behavior, race detection is required.

## Local prerequisites

- A supported Go toolchain chosen and documented by the fork. The inherited module and CI specify Go 1.18, which should be treated as a baseline to migrate from, not an implicit long-term support decision.
- `make` for convenience targets.
- VHS only when regenerating `assets/tui.gif` or `assets/inline.gif` from their `.tape` files.
- Docker only when building/running the inherited container workflow.

## Standard commands

The inherited Makefile delegates to the following commands:

```sh
make build       # go build with build metadata ldflags
make test        # go test ./...
make install     # go install with build metadata ldflags
make gif         # render VHS recordings
```

For a normal code change, use targeted package tests first, then the full suite once:

```sh
go test ./source ./downloader ./converter/...
go test -race ./...
go vet ./...
```

Run a CLI smoke test after building. It must not depend on a live source when validating non-provider changes:

```sh
./mangal version
./mangal where
./mangal config info
./mangal inline schema
```

Provider availability is operational evidence, not deterministic unit-test evidence. Live provider checks should be opt-in and separate from required CI.

## Test design rules

1. Use `httptest.Server` for HTTP status, timeout, size, and body-close behavior.
2. Use temporary directories/filesystems rather than global persistent paths.
3. Use fixture HTML/JSON for built-in provider parsing. Store the upstream request URL and capture date with each fixture.
4. Test observable contracts: paths, output artifacts, progress/error outcomes, JSON payloads, and cleanup. Do not test incidental helper implementation.
5. Run `go test -race` for every path with goroutines, channels, shared model state, or global infrastructure.
6. Keep live tests behind an explicit build tag, environment variable, or dedicated workflow with clear rate limits and failure reporting.

## Release policy

Do not tag or publish this fork until identity cutover is complete. The inherited `.goreleaser.yaml` targets upstream Docker, Homebrew, Scoop, GitHub, documentation, and install endpoints.

A fork release checklist should include:

- canonical module/import path is used by all first-party Go code and ldflags;
- release config points only to fork-owned artifacts and credentials;
- README, install scripts, command help, completions, container labels, support links, and generated metadata use fork identity;
- full deterministic test suite, race suite, CLI smoke checks, and release dry run pass;
- SBOM/license/dependency policy has been executed and recorded;
- checksums are published and verified from a clean build;
- plugin manifest and installer provenance are verified; and
- release notes state provider additions/removals, inline-schema changes, migrations, and known source outages.

Pin GitHub Actions and release tools to immutable versions/commit SHAs. A release must not modify dependency files (`go mod tidy`) or depend on a floating `latest` tool version.

## Provider maintenance policy

Every provider should have an owner, a contract fixture, supported capabilities, and a deprecation path.

| Event | Required response |
| --- | --- |
| Parsing regression | Capture sanitized fixture, reproduce deterministically, add a regression test, release a narrow fix. |
| Rate limit/block | Surface a typed user error; reduce/review request policy; do not hide failures with retries that amplify traffic. |
| Site domain/API migration | Update provider ID/provenance carefully; keep library manifests usable; announce any source-ID compatibility impact. |
| Unmaintained provider | Mark deprecated, warn in source list/status, then remove on an announced schedule. |
| Third-party scraper contribution | Review manifest, immutable reference, digest/signature, declared capabilities, and fixtures before publication. |

Do not describe a mutable remote scraper registry as "official" unless the fork actually owns, secures, and maintains that registry.

## Credentials and user data

- Treat the AniList client secret and OAuth PIN as secrets. They remain plaintext in the normal TOML configuration file, but on Unix the configuration directory is enforced as `0700` and the file as `0600` after creation and every write, including legacy files. On Windows, storage security depends on the account ACLs; the application does not change them. No keychain is used.
- `config get`, `config info`, and `env` retain complete key/environment listings while masking sensitive current values.
- Store only the user data necessary for history and provider operation. Document cache/history/config locations through `mangal where`.
- Add an export/delete command before expanding persistent library or account data.
- Support offline use after a chapter is committed; network source availability must not be required to read/verify local artifacts.

Configuration permission checks are limited to the configuration directory and TOML file. Cache, history, log, source, download, and temporary paths retain their existing permissions.

`MANGAL_CONFIG_PATH` must point to a dedicated private directory. On Unix, a missing custom directory is created as `0700`; an existing custom directory must already be `0700` and is rejected rather than silently changed. The default application config directory may be migrated to `0700`.

For this storage-hardening lane, run the focused checks on Unix and a Windows compile check from any host:

```sh
go test ./config ./where
GOOS=windows go test -c ./config ./where
```

## Documentation contract

Update documentation in the same change when any of these change:

- executable/product/module name, installation channel, supported platforms, or release ownership;
- CLI flags, JSON schemas, stdout/stderr or exit-code behavior;
- configuration keys/defaults/migration behavior;
- output layout, generated metadata, or manifest schema;
- provider support/provenance/capability policy;
- reader, AniList, credential, history, or privacy behavior; or
- validation/release commands.

The document set is organized as follows:

- `docs/ARCHITECTURE.md` — current implementation and boundaries.
- `docs/CODEBASE_CATALOG.md` — package and operational inventory.
- `docs/CODE_REVIEW.md` — findings and remediation sequence.
- `docs/TECHNICAL_STRATEGY.md` — forward technical direction and milestones.
- `docs/MAINTENANCE.md` — this operational guide.

Keep findings evidence-based. Label unverified behavior as an inference and distinguish static observations from executed validation.
