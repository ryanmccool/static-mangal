# Codebase catalog

This catalog describes first-party repository content at review time. `vendor/` is a vendored dependency snapshot and is intentionally excluded from the package descriptions below.

## Root files and generated assets

| Path | Role |
| --- | --- |
| `main.go` | Process entrypoint: initialize configuration and logging, then execute Cobra. |
| `go.mod`, `go.sum`, `vendor/` | Go 1.18 module, dependency locks, and vendored dependencies. Module path is still `github.com/metafates/mangal`. |
| `README.md` | Upstream-oriented product/install/usage document; it includes an explicit upstream-maintenance warning and upstream URLs. |
| `CHANGELOG.md` | Upstream release history through 4.0.6. |
| `LICENSE` | MIT license with upstream copyright attribution. |
| `Makefile` | `build`, `install`, `test`, `uninstall`, and VHS GIF generation targets. |
| `Dockerfile` | Container build/run definition. |
| `.goreleaser.yaml` | Cross-platform binaries, archives, Docker, Homebrew, Scoop, packages, GitHub release, checksums, and changelog setup. Targets are upstream-owned. |
| `completions/` | Generated shell completions for Bash, Fish, PowerShell, and Zsh. |
| `assets/` | TUI/inline VHS tapes, rendered GIFs, JSON schemas, and test data. |
| `scripts/` | Upstream install/run shell and PowerShell scripts. |
| `.github/workflows/test.yml` | Cross-platform tests on Go 1.18; non-Windows runs with `-race`. |
| `.github/workflows/release.yml` | Tag/manual release pipeline; uses older GitHub Actions and invokes GoReleaser. |
| `.github/scripts/generate-release-notes.py` | Release-note helper. |

## First-party package map

| Package/path | Responsibilities | Principal entrypoints or types |
| --- | --- | --- |
| `cmd/` | Cobra command tree, flags, validation, human-facing errors | root command; `config`, `clear`, `env`, `inline`, `integration`, `mini`, `run`, `sources`, `version`, `where` |
| `config/` | Viper setup, defaults, environment binding, TOML presentation | `Setup`, `Default`, `Field` |
| `key/` | String constants for all 53 Viper keys | `DefinedFieldsCount` and grouped constants |
| `constant/` | Product name, build metadata, format strings, OS/template constants | build ldflag targets and format constants |
| `where/` | Derives and creates config/cache/download/temp paths | `Config`, `Cache`, `Downloads`, `Temp` |
| `filesystem/` | Process-global Afero filesystem and adapter for gache | `Api`, `SetMemMapFs`, `SetOsFs`, `GacheFs` |
| `network/` | Shared HTTP client/transport for most application requests | `Client` with one-minute client timeout |
| `log/` | Logrus configuration and wrappers | `Setup`, level/json/file behavior |
| `color/`, `style/`, `icon/` | Terminal color, Lip Gloss styles, selectable icon sets | style builders, `icon.Get` |
| `query/` | Query normalization, remembered suggestions, fuzzy suggestion output | `Remember`, `SuggestMany` |
| `source/` | Core mutable domain model; page downloads; destination names; metadata mapping | `Source`, `Manga`, `Chapter`, `Page`, `ComicInfo`, `SeriesJSON` |
| `provider/` | Provider registry and custom source discovery | `Provider`, `Builtins`, `Customs`, `Get` |
| `provider/mangadex/` | MangaDex API-backed source | `New`, search/chapters/pages/cache helpers |
| `provider/generic/` | Configured HTML scraper engine using Colly/GoQuery | `Configuration`, `Extractor`, `Scraper`, `New` |
| `provider/manganelo/`, `provider/manganato/`, `provider/mangapill/` | Per-site selectors, URL generation, pacing, and chapter ordering | package `Config` values consumed by generic engine |
| `provider/custom/` | GopherLua compilation, required-function validation, model translation, custom-provider caching | `LoadSource`, `Compile`, `luaSource` |
| `installer/` | GitHub repository-tree collection and Lua-scraper download/install | `Scrapers`, `Scraper.Install` |
| `downloader/` | Fetch pages, export chapters, read existing/temp output, write history | `Download`, `Read` |
| `converter/` | Output converter registry | `Converter`, `Get`, `Available` |
| `converter/pdf/` | PDF export via pdfcpu | `PDF.Save`, `SaveTemp` |
| `converter/cbz/` | CBZ archive and optional ComicInfo generation | `CBZ.Save`, `SaveTo` |
| `converter/zip/` | ZIP export | `ZIP.Save`, `SaveTo` |
| `converter/plain/` | Directory/page-file export | `Plain.Save`, `SaveTemp` |
| `open/` | OS-specific reader/browser command execution | `Open`, `Run`, `Start` |
| `history/` | Cached JSON reading history and optional async integration trigger | `Get`, `Save`, `Remove`, `SavedChapter` |
| `anilist/` | GraphQL search, closest-title resolution, local bindings/cache, metadata model | `SearchByName`, `GetByID`, `FindClosest`, `SetRelation` |
| `integration/` | Service-neutral progress interface and global AniList implementation | `Integrator`, `Anilist` |
| `integration/anilist/` | OAuth PIN token exchange and `SaveMediaListEntry` mutation | `Anilist`, `AuthURL`, `MarkRead` |
| `inline/` | Scriptable selection parsing, model preparation, JSON output | `Run`, `Options`, manga/chapter selector parsers |
| `mini/` | Lightweight interactive reader flow | input/binding/state/UI helpers |
| `tui/` | Bubble Tea state machine, list items, key maps, rendering, asynchronous handlers | `Run`, `statefulBubble`, `Update`, handler commands |
| `update/` | Rebuilds series metadata in existing chapter outputs | `Metadata`, `GetName`, chapter/comicinfo helpers |
| `util/` | Filename/path helpers, stack, terminal helpers, ZIP extraction | `SanitizeFilename`, `Stack`, `Unzip` |
| `version/` | Current-version display, semantic comparison, optional release notification | `Version`, `Compare`, `Notify` |

## CLI surface

### Root behavior and shared flags

`mangal` starts the full-screen TUI. `mangal --continue` opens saved history. Persistent flags select output format, icon style, history-on-read behavior, and default source(s); `--version` prints version information.

### Commands

| Command | Purpose |
| --- | --- |
| `clear` | Clears cache/log/temp-like application files selected by flags. |
| `config info/get/set/write/delete/reset` | Inspects and changes Viper-backed TOML configuration. |
| `env` | Lists configuration-derived environment variable names and values. |
| `inline` | Script-oriented search/select/read/download/JSON command. Supports JSON schemas and AniList subcommands. |
| `integration anilist` | Enables/disables AniList integration and assists with authentication settings. |
| `mini` | Starts the compact interactive mode. |
| `run [file]` | Runs a Lua file with custom-provider support libraries; intended for scraper debugging/standalone Lua execution. |
| `sources list/remove/install/gen` | Lists built-in/custom sources, removes installed Lua files, browses remote scrapers through the TUI, or generates a source file. |
| `version` | Displays build version and platform details. |
| `where` | Prints application paths. |

### Inline-mode contract

`mangal inline` is the automation-facing surface. It requires a query and normally a manga selector. It supports chapter selectors (`first`, `last`, `all`, numeric index/range, and substring), optional page and AniList-metadata population, JSON output, file output, chapter download, and an AniList management subtree. JSON schemas are generated at runtime from `inline.Output` or AniList types; static schema examples are in `assets/`.

Any fork change to fields, selector semantics, output ordering, exit codes, or stderr behavior is a user-visible API change and should be versioned and fixture-tested.

## Data contracts and file names

- `source.Manga`, `Chapter`, and `Page` serialize with lower-camel-case JSON fields for inline output. Back-references and binary page content are excluded.
- Chapter filenames derive from `downloader.chapter_name_template`, are sanitized, and receive the configured format extension except for the plain-directory format.
- CBZ output may contain page files plus `ComicInfo.xml`; ZIP contains pages; PDF contains converted page images; plain contains page files in a chapter directory.
- `series.json` is generated per manga when enabled. Cover files are named `cover` plus an extension inferred from the AniList URL, defaulting to `.jpg`.
- History is a map encoded through `gache`; key format is owned by `history.SavedChapter.encode`.

## Build, test, and release contract

### Local

The Makefile delegates to Go:

```sh
make build
make test
make install
```

Build metadata is injected by `-ldflags` into the `constant` package. `make gif` requires VHS.

### Continuous integration

The test workflow runs `go test -race -v ./...` on Linux/macOS and `go test -v ./...` on Windows. It pins Go 1.18 and uses `actions/checkout@v3` plus `actions/setup-go@v3`.

### Release

GoReleaser cross-compiles static binaries for Linux, Windows, and macOS across 386/amd64/arm/arm64. It publishes archives, checksums, an amd64 Docker image, Homebrew/Scoop updates, Debian/RPM packages, and a GitHub release. Every ownership, URL, image, package-tap, and module-path reference must be rehomed before this fork releases.

## Test inventory and gaps

Existing tests cover basic format output, filesystem switching, history CRUD, icon variants, selected filename/metadata behavior, utility helpers, semantic version comparison, paths, and several provider/AniList live calls. The provider and AniList tests depend on current third-party availability and titles; they are not deterministic fixtures.

Not covered by focused local tests:

- the download orchestration state machine, error handling, and partial-output cleanup;
- async page downloading, cancellation, bounded concurrency, and race freedom;
- TUI channel/error transitions;
- scraper installation provenance, failure, digest verification, or unsafe-code policy;
- HTTP status/body closure behavior and response size limits;
- configuration migration and fork identity/release correctness;
- output-contract fixtures for inline JSON; and
- archive path traversal rejection.
