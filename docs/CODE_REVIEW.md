# Code review

## Scope and confidence

Static review of first-party code, tests, CI, release configuration, scripts, and Markdown. Vendored dependency source was not audited. The current environment does not provide `go`, so `go test`, `go test -race`, compilation, dependency scanning, and live-provider verification were not run. Severity reflects user impact and exploitability of the observed code paths, not a claim of observed exploitation.

## Executive assessment

The code is a capable but dormant upstream CLI with valuable product seams. It is not release-ready as a new fork. Fix the fork identity, data-race/panic/path defects, remote scraper trust model, and reproducible validation before adding provider or UI features.

## Findings

### Critical — fork release machinery still publishes as upstream

**Evidence:** `go.mod` declares `github.com/metafates/mangal`; all first-party imports use that module. `.goreleaser.yaml` still sets upstream ldflag paths, Docker image (`metafates/mangal`), Homebrew and Scoop targets, GitHub release owner/repository, homepage, installer URL, and maintainer. `Makefile`, `README.md`, command help, and scripts retain upstream URLs and install commands.

**Impact:** A release can fail, publish to the wrong destination if credentials permit, advertise an unowned installer, generate incorrect version metadata, or send users to upstream documentation. Retaining the upstream module path also prevents a clean downstream Go import identity.

**Recommendation:** Make fork cutover the first commit series: choose canonical product name, module path, GitHub owner/repo, release channels, container registry, scraper registry, and support URL; replace every first-party import and metadata reference; regenerate completions/assets if command identity changes; publish only after a dry-run release in an isolated test repository. Preserve upstream copyright/license notices and add fork attribution rather than overwriting them.

### Critical — custom scraper installation executes mutable remote code without provenance controls

**Evidence:** `installer/api.go` obtains a GitHub tree from configurable `installer.user`, `installer.repo`, and `installer.branch`. `installer/scraper.go` downloads content from URLs returned by that tree and writes executable `.lua` files. `provider/custom/loader.go` compiles and executes these files in-process after `mangal-lua-libs` preloads modules. There is no immutable commit pin, digest, signature, manifest, confirmation of permissions, or recorded source provenance.

**Impact:** A compromise of the configured repository/branch, DNS/account ownership change, or a user-configured malicious repository can run code inside the Mangal process. The current UI describes this as an official source browser, which understates the trust decision.

**Recommendation:** Replace branch-based discovery with a signed manifest pinned to an immutable commit or release artifact. Record provider name, version, commit, SHA-256, signing key, install timestamp, and enabled/disabled state. Verify before install and load; display provenance and permissions; require explicit opt-in before enabling network/browser-capable plugins. Longer term, move scraping to a narrowly scoped process with a stable JSON-RPC contract instead of executing third-party code in the main process.

### High — asynchronous page download has data races and a nil-pointer panic path

**Evidence:** In `source/chapter.go:DownloadPages`, asynchronous mode starts one goroutine per page. Goroutines concurrently read/write the named `err`, increment `c.size`, and call `progress(status())`, which reads `c.size`, without synchronization. The nil guard dereferences its nil receiver: `if page == nil { return fmt.Errorf(..., page.Index) }`.

**Impact:** `go test -race` should flag this path. Errors can be lost or observed inconsistently, reported size is nondeterministic, UI progress is unsafe, and a malformed provider page slice panics rather than returning an error. Page fan-out is unbounded per chapter.

**Recommendation:** Replace the shared mutable loop with a bounded worker pool owned by a `context.Context`. Send per-page results on a channel; aggregate error, bytes, and progress from one coordinator goroutine; cancel workers after the first fatal error; reject nil pages using the slice position, not `page.Index`. Add race-enabled tests for success, first failure, cancellation, nil pages, known/unknown content length, and bounded concurrency.

### High — volume directory path is constructed from an empty base

**Evidence:** `source/chapter.go:path` sets `path = filepath.Join(path, util.SanitizeFilename(c.Volume))` when volume directories are enabled. At that point named return `path` is empty; it creates a relative volume directory, then returns `filepath.Join(relativeTo, c.Filename())`, discarding the volume component.

**Impact:** `downloader.create_volume_dir=true` creates an unintended directory relative to the process working directory while storing chapters outside it. This violates a documented configuration behavior and can create clutter or unexpectedly affect a caller's directory.

**Recommendation:** Join the sanitized volume onto `relativeTo`, assign that as the output base, create it there, then append the filename. Cover both archive and plain formats, temp and permanent paths, no-volume chapters, and paths with unsafe volume text in table-driven tests.

### High — TUI concurrent handlers mutate shared state without synchronization

**Evidence:** `tui/handlers.go:loadSources` shares `err` and `b.progressStatus` across source-loading goroutines. `searchManga` concurrently appends to the shared `mangas` slice and also mutates `b.progressStatus`; it sends errors through an unbuffered shared channel while worker completion continues. Other handlers update `statefulBubble` fields directly from Tea commands. `statefulBubble` itself owns several unbuffered completion/error channels.

**Impact:** Searches across multiple selected providers can race, lose/corrupt slice state, or produce confusing error and completion ordering. Some error paths can leave a worker blocked while it attempts to send a second message after the UI selected the error path. The TUI's model is intended to be event-driven, but work goroutines mutate it outside its update loop.

**Recommendation:** Make background commands return immutable result/error messages to Bubble Tea; mutate `statefulBubble` only in `Update`. Use `errgroup.WithContext` or a coordinator to collect source results deterministically, cancel on the chosen policy, and return one completion message. Eliminate shared error/completion channels where Tea messages suffice. Add race tests for two providers, one provider failure, cancellation/back navigation, and repeated searches.

### High — temporary output lifecycle is globally shared and deletion is unsynchronized

**Evidence:** `cmd/root.go` starts `util.Delete(where.Temp())` in a goroutine during package initialization. `where.Temp()` is a fixed OS-temp `mangal` directory. `source.Manga.Path(true)` caches that same directory, not a per-operation directory.

**Impact:** The application can begin a read/conversion while the asynchronous startup cleanup removes its shared temp directory. Concurrent temporary reads can collide when chapter filenames overlap. A stale or partially written temp artifact is indistinguishable from another operation's file.

**Recommendation:** Remove the startup deletion goroutine. Create an operation-specific temporary directory with `MkdirTemp`, pass its ownership through the reader/converter flow, and remove it synchronously in a deferred cleanup after the external reader launch policy is resolved. Test collision isolation and startup/read ordering.

### High — HTTP error handling falsely reports failed cover downloads as success

**Evidence:** `source/manga.go:DownloadCover` uses `http.Get`; when the status is not 200 it logs and returns the existing `err`, which is nil after a successful transport call. It also uses the default client rather than `network.Client` and reads the complete body without a cap.

**Impact:** A 404/429/500 cover response can be treated as success and suppress retries because `coverDownloaded` was set before the request. Requests can hang under the default client's lack of a timeout.

**Recommendation:** Use a single injected HTTP client with context, transport policy, response-body closure, status-specific errors, content-type/size checks, and limited readers. Set `coverDownloaded` only after a verified successful write; set metadata/cover idempotence flags only after their work succeeds. Test non-200, timeout, invalid body, existing cover, and retry behavior.

### High — downloaded data is buffered wholly in memory with insufficient resource limits

**Evidence:** `source/page.go:Download` reads every page into `bytes.Buffer`; if `Content-Length` is unknown it calls `io.ReadAll`, and if known it allocates exactly that size. `DownloadPages` can launch every page at once. Generic-provider collectors allow high configured parallelism; the shared HTTP transport permits up to 200 connections per host.

**Impact:** A malformed/hostile response or a long chapter can consume excessive memory and connections. The process has no per-page size limit, chapter byte budget, cancellation propagation, or backpressure between fetching and exporting.

**Recommendation:** Stream each page to an operation-scoped staging directory with maximum page/chapter budgets; validate status, type, image decode, and actual byte count; use bounded concurrency and context cancellation. Keep an optional in-memory path only for deliberately small fixtures. Make limits visible in configuration with conservative defaults.

### Medium — several network paths leak response bodies or bypass the shared client

**Evidence:** `installer/api.go:collect`, `installer/scraper.go:download`, and `integration/anilist/login.go` do not close `resp.Body`; they also return from status failures before any close. `integration/anilist/mark.go` uses `http.DefaultClient` and does not close the body. Installer and cover requests use `http.Get`, bypassing `network.Client` timeouts and transport settings.

**Impact:** Repeated operations can exhaust idle connections/file descriptors and have inconsistent timeout policy.

**Recommendation:** Centralize request execution behind a context-aware client helper that always closes bodies, limits reads, maps non-success status to typed errors, and emits safe structured request telemetry. Unit-test body closure and timeout behavior with `httptest.Server`.

### Medium — history persistence is a read-modify-write store with no coordination

**Evidence:** `history.Save` and `history.Remove` call `Get`, modify a map, then call `cacher.Set` without locking or atomic rename ownership. `downloader.Download` invokes `history.Save` in a goroutine. AniList marking starts another goroutine from history saving.

**Impact:** Concurrent operations can overwrite each other's history entries, complicate shutdown/error reporting, and make local state nondeterministic.

**Recommendation:** Make persistence synchronous at the workflow boundary or use a single serialized history writer. Write atomically through a temporary file plus rename and lock/process-coordinate if simultaneous processes are supported. Queue AniList synchronization separately with durable retry state rather than hiding it in a goroutine.

### Medium — metadata and provider caches turn transient failures into sticky results

**Evidence:** `Manga.PopulateMetadata` sets `m.populated = true` before AniList lookup succeeds. `Manga.DownloadCover` sets `m.coverDownloaded = true` before a URL is resolved/downloaded. `provider/generic` caches result maps by URL with no expiry or error representation.

**Impact:** A temporary endpoint failure prevents retry for the lifetime of the model; callers cannot distinguish cache hit, empty remote result, or failed parsing.

**Recommendation:** Track explicit state (`notStarted`, `inProgress`, `ready`, `failed`) with an error/time and retry policy. Cache only verified results, scope cache lifetime deliberately, and return typed provider errors that retain source and URL context.

### Medium — archive extraction containment check is incorrect for relative traversal

**Evidence:** `util.Unzip` joins `dest` and `f.Name`, then checks a prefix against `filepath.Clean(dest) + separator`. For a relative destination such as `a`, `filepath.Join("a", "../outside")` is `a/../outside`, which passes the prefix check before filesystem resolution escapes `a`.

**Impact:** The current callers switch to an in-memory filesystem before extraction, reducing immediate disk impact, but the utility itself is not safe if reused with the OS filesystem.

**Recommendation:** Compute absolute cleaned destination and absolute cleaned candidate, then require `filepath.Rel(dest, candidate)` to be neither `..` nor a path beginning with `..` plus a separator. Reject absolute archive names. Add malicious archive fixtures.

### Medium — release, CI, and dependency maintenance are stale

**Evidence:** `go.mod` pins Go 1.18; test CI uses `actions/checkout@v3` and `actions/setup-go@v3`; release CI uses checkout/setup-go v2, docker actions v1, Python setup v4, and GoReleaser action v2 with `version: latest`. GoReleaser runs `go mod tidy` before a release, allowing release-time dependency-file changes.

**Impact:** The fork lacks a current supported Go/tool-action baseline, reproducible release input, and an explicit dependency vulnerability/update process. This review did not run a vulnerability scanner, so it makes no CVE claim.

**Recommendation:** Select a supported Go version, update actions to maintained major versions pinned to immutable commit SHAs, pin GoReleaser, remove mutating `go mod tidy` from release builds, enforce a clean lockfile in CI, and add scheduled dependency/vulnerability review. Keep `vendor/` either regenerated and verified or remove it consistently.

### Medium — tests exercise unstable external services and miss core safety contracts

**Evidence:** AniList and provider tests query live services/titles. Existing tests do not cover downloader orchestration, page concurrency, UI handler sequencing, scraper provenance, network resource handling, or inline JSON fixtures.

**Impact:** CI can fail due to unrelated third-party changes while local correctness regressions pass. Race testing in CI is valuable but does not currently execute targeted coverage for the observed unsafe paths.

**Recommendation:** Split deterministic unit/integration tests from opt-in live-provider smoke tests. Use `httptest.Server`, checked-in HTML/API fixtures, a fake `Source`, and temporary filesystems. Add `go test -race ./...`, JSON schema/golden compatibility tests, and a minimal CLI smoke command to CI.

### Low — global mutable infrastructure limits reuse and test isolation

**Evidence:** Viper, the Afero filesystem, converter registry, network client, history cache, and AniList integration are package globals. Commands frequently call `os.Exit` through `handleErr`.

**Impact:** Tests are order-sensitive and parallelization is constrained. The domain/download code cannot be cleanly embedded in another Go application or invoked repeatedly with isolated configuration.

**Recommendation:** Introduce an `App`/service dependency container at command startup. Inject configuration, filesystem, HTTP client, clock, provider registry, logger, and history sink into workflows; reserve `os.Exit` for `main`.

## Prioritized remediation order

1. **Fork cutover:** canonical identity, module/import path, docs, installer/release targets, CI ownership; do not publish before this is complete.
2. **Safety patch:** fix volume path, nil-page panic, page-download race, startup temp deletion, cover status error, and all response-body closures; add focused tests and run race detection.
3. **Trust and resilience baseline:** signed/pinned scraper manifest, unified HTTP policy, resource bounds, deterministic provider fixtures, and a clear unsafe-plugin policy.
4. **Workflow architecture:** context-aware operation service, worker pool, transactional staging/commit, serialized history/AniList queue, and TUI message-only background work.
5. **Maintenance baseline:** supported Go/actions/release tooling, reproducible vendor/dependency policy, SBOM/license/vulnerability routine, and separate live scraper monitoring.

## Strengths worth retaining

- A compact Go binary with cross-platform release intent and no mandatory runtime daemon.
- A clean conceptual `source.Source` provider interface.
- Both rich interactive and scriptable JSON surfaces.
- Practical export formats and metadata artifacts familiar to manga/comic-reader tooling.
- Existing configuration descriptions, shell completions, schemas, basic converter tests, and multi-platform CI as a starting foundation.
