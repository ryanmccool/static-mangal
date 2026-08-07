// Package selfupdate implements the release verification and replacement
// primitives used by the command-line updater.
package selfupdate

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	// DefaultAPIURL is the canonical GitHub latest-release endpoint.
	DefaultAPIURL = "https://api.github.com/repos/ryanmccool/static-mangal/releases/latest"

	maxDocumentSize = 2 << 20
	maxArchiveSize  = 256 << 20

	defaultHTTPTimeout = 30 * time.Second
	validationTimeout  = 5 * time.Second
	validationOutput   = 4 << 10

	binaryName   = "static-mangal"
	checksumName = "checksums.txt"
)

func supportedArch(arch string) bool {
	switch arch {
	case "386", "amd64", "arm", "arm64":
		return true
	default:
		return false
	}
}

var (
	// ErrUnsupportedPlatform is returned when Apply is requested for an OS
	// without the Unix replacement transaction implemented by this package.
	ErrUnsupportedPlatform = errors.New("selfupdate: unsupported platform")
	// ErrUnsupportedDistribution is returned unless the archive distribution is
	// explicitly selected.
	ErrUnsupportedDistribution = errors.New("selfupdate: unsupported distribution")
	// ErrLockExists is returned when another update transaction, or a previous
	// crashed transaction, left the update lock behind.
	ErrLockExists = errors.New("selfupdate: update lock exists")
	// ErrChecksumMismatch is returned when the downloaded archive does not match
	// its published checksum.
	ErrChecksumMismatch = errors.New("selfupdate: archive checksum mismatch")
	// ErrLocalAhead is returned when the installed version is newer than the
	// latest stable release. The updater never downgrades.
	ErrLocalAhead = errors.New("selfupdate: local version is newer than latest release")
)

// ValidationFunc validates the newly installed executable. path is the
// executable path and expectedVersion is the release version without its v
// prefix.
type ValidationFunc func(ctx context.Context, path, expectedVersion string) error

// Config controls a Check or Apply operation. Release discovery, platform,
// architecture, and executable-path resolution are deliberately not
// configurable by callers. CurrentVersion and Distribution are explicit.
type Config struct {
	CurrentVersion string
	Distribution   string
	Validate       ValidationFunc
}

// Result reports the local and latest stable release versions. Versions are
// returned in MAJOR.MINOR.PATCH form, without the release tag's v prefix.
type Result struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
}

// UnsupportedPlatformError identifies the configured OS that cannot be
// updated by Apply.
type UnsupportedPlatformError struct {
	GOOS string
}

func (e *UnsupportedPlatformError) Error() string {
	return fmt.Sprintf("%v: %s", ErrUnsupportedPlatform, e.GOOS)
}

func (e *UnsupportedPlatformError) Unwrap() error { return ErrUnsupportedPlatform }

// RollbackError reports both the update failure and a failure to restore the
// previous executable.
type RollbackError struct {
	UpdateErr   error
	RollbackErr error
}

func (e *RollbackError) Error() string {
	return fmt.Sprintf("selfupdate: update failed: %v; rollback failed: %v", e.UpdateErr, e.RollbackErr)
}

func (e *RollbackError) Unwrap() error { return e.UpdateErr }

// CommittedUpdateError reports a warning after the replacement became
// irreversible. The caller must not describe this as an update failure.
type CommittedUpdateError struct {
	WarningErr error
}

func (e *CommittedUpdateError) Error() string {
	return fmt.Sprintf("selfupdate: update committed; cleanup or durability warning: %v", e.WarningErr)
}

func (e *CommittedUpdateError) Unwrap() error { return e.WarningErr }

type version struct {
	major string
	minor string
	patch string
}

func (v version) String() string {
	return v.major + "." + v.minor + "." + v.patch
}

func compareVersion(a, b version) int {
	for _, pair := range [][2]string{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if len(pair[0]) != len(pair[1]) {
			if len(pair[0]) < len(pair[1]) {
				return -1
			}
			return 1
		}
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}

func parseVersionPart(part string) (string, error) {
	if part == "" || (len(part) > 1 && part[0] == '0') {
		return "", errors.New("version component is not a canonical number")
	}
	for i := 0; i < len(part); i++ {
		if part[i] < '0' || part[i] > '9' {
			return "", errors.New("version component is not numeric")
		}
	}
	return part, nil
}

func parseCurrentVersion(raw string) (version, error) {
	if strings.HasPrefix(raw, "v") {
		raw = raw[1:]
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("invalid current version %q: expected MAJOR.MINOR.PATCH", raw)
	}
	major, err := parseVersionPart(parts[0])
	if err != nil {
		return version{}, fmt.Errorf("invalid current version %q: %w", raw, err)
	}
	minor, err := parseVersionPart(parts[1])
	if err != nil {
		return version{}, fmt.Errorf("invalid current version %q: %w", raw, err)
	}
	patch, err := parseVersionPart(parts[2])
	if err != nil {
		return version{}, fmt.Errorf("invalid current version %q: %w", raw, err)
	}
	return version{major: major, minor: minor, patch: patch}, nil
}

func parseReleaseTag(raw string) (version, error) {
	if !strings.HasPrefix(raw, "v") {
		return version{}, fmt.Errorf("invalid release tag %q: expected vMAJOR.MINOR.PATCH", raw)
	}
	if strings.HasPrefix(raw[1:], "v") {
		return version{}, fmt.Errorf("invalid release tag %q: expected vMAJOR.MINOR.PATCH", raw)
	}
	v, err := parseCurrentVersion(raw[1:])
	if err != nil {
		return version{}, fmt.Errorf("invalid release tag %q: expected vMAJOR.MINOR.PATCH", raw)
	}
	return v, nil
}

// transactionFS is deliberately package-private. It is a narrow seam for
// deterministic transaction-failure tests; production callers cannot replace
// filesystem operations or target discovery.
type transactionFS struct {
	acquireLock func(string) (string, error)
	lstat       func(string) (os.FileInfo, error)
	link        func(string, string) error
	rename      func(string, string) error
	remove      func(string) error
	syncDir     func(string) error
}

func productionTransactionFS() transactionFS {
	return transactionFS{
		acquireLock: createLock,
		lstat:       os.Lstat,
		link:        os.Link,
		rename:      os.Rename,
		remove:      os.Remove,
		syncDir:     syncDirectory,
	}
}

func normalizeTransactionFS(fs transactionFS) transactionFS {
	production := productionTransactionFS()
	if fs.acquireLock == nil {
		fs.acquireLock = production.acquireLock
	}
	if fs.lstat == nil {
		fs.lstat = production.lstat
	}
	if fs.link == nil {
		fs.link = production.link
	}
	if fs.rename == nil {
		fs.rename = production.rename
	}
	if fs.remove == nil {
		fs.remove = production.remove
	}
	if fs.syncDir == nil {
		fs.syncDir = production.syncDir
	}
	return fs
}

type resolvedConfig struct {
	current  version
	goos     string
	goarch   string
	target   string
	apiURL   string
	client   *http.Client
	validate ValidationFunc
	fs       transactionFS
	origin   string
}

type testSettings struct {
	apiURL     string
	client     *http.Client
	goos       string
	goarch     string
	target     string
	origin     string
	executable func() (string, error)
	fs         transactionFS
}

func productionSettings() testSettings {
	return testSettings{goos: runtime.GOOS, goarch: runtime.GOARCH}
}

func resolveConfig(c Config, needTarget bool, settings testSettings) (resolvedConfig, error) {
	current, err := parseCurrentVersion(c.CurrentVersion)
	if err != nil {
		return resolvedConfig{}, err
	}

	goos := settings.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := settings.goarch
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	apiURL := settings.apiURL
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}
	apiURL, err = latestAPIURL(apiURL)
	if err != nil {
		return resolvedConfig{}, err
	}

	target := settings.target
	if needTarget && target == "" {
		executable := settings.executable
		if executable == nil {
			executable = defaultExecutable
		}
		target, err = executable()
		if err != nil {
			return resolvedConfig{}, fmt.Errorf("resolve executable path: %w", err)
		}
	}
	if target != "" {
		target, err = filepath.Abs(target)
		if err != nil {
			return resolvedConfig{}, fmt.Errorf("resolve executable path: %w", err)
		}
	}

	return resolvedConfig{
		current:  current,
		goos:     goos,
		goarch:   goarch,
		target:   filepath.Clean(target),
		apiURL:   apiURL,
		client:   boundedClient(settings.client, settings.origin),
		validate: c.Validate,
		fs:       normalizeTransactionFS(settings.fs),
		origin:   settings.origin,
	}, nil
}

func defaultExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executableInfo, err := os.Lstat(executable)
	if err != nil {
		return "", err
	}
	launch := os.Args[0]
	if launch == "" {
		return "", errors.New("selfupdate: launch path is empty")
	}
	if !filepath.IsAbs(launch) {
		if strings.ContainsRune(launch, os.PathSeparator) {
			launch, err = filepath.Abs(launch)
		} else {
			launch, err = exec.LookPath(launch)
		}
		if err != nil {
			return "", fmt.Errorf("resolve launch path: %w", err)
		}
	}
	executable, launch, err = resolveExecutablePaths(executable, launch)
	if err != nil {
		return "", err
	}
	launchInfo, err := os.Lstat(launch)
	if err != nil {
		return "", err
	}
	executableInfo, err = os.Lstat(executable)
	if err != nil {
		return "", err
	}
	if !os.SameFile(executableInfo, launchInfo) {
		return "", errors.New("selfupdate: launch path and executable identity differ")
	}
	return executable, nil
}

func resolveExecutablePaths(executable, launch string) (string, string, error) {
	resolvedExecutable, err := filepath.Abs(executable)
	if err != nil {
		return "", "", fmt.Errorf("resolve executable path: %w", err)
	}
	if err := rejectSymlinkComponents(resolvedExecutable, "default executable path"); err != nil {
		return "", "", err
	}
	resolvedLaunch, err := filepath.Abs(launch)
	if err != nil {
		return "", "", fmt.Errorf("resolve launch path: %w", err)
	}
	if err := rejectSymlinkComponents(resolvedLaunch, "launch path"); err != nil {
		return "", "", err
	}
	return filepath.Clean(resolvedExecutable), filepath.Clean(resolvedLaunch), nil
}

func rejectSymlinkComponents(filename, label string) error {
	filename, err := filepath.Abs(filename)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", label, err)
	}
	for current := filepath.Clean(filename); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", label, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("selfupdate: %s has a symlinked component", label)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func boundedClient(input *http.Client, testOrigin string) *http.Client {
	if input == nil {
		input = defaultHTTPClient()
	}
	client := *input
	if client.Timeout <= 0 || client.Timeout > defaultHTTPTimeout {
		client.Timeout = defaultHTTPTimeout
	}
	originalRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL == nil || req.URL.Scheme != "https" {
			return errors.New("selfupdate: refusing non-HTTPS redirect")
		}
		if len(via) >= 10 {
			return errors.New("selfupdate: too many redirects")
		}
		if originalRedirect != nil {
			if err := originalRedirect(req, via); err != nil {
				return err
			}
			if req.URL == nil || req.URL.Scheme != "https" {
				return errors.New("selfupdate: refusing non-HTTPS redirect")
			}
		}
		initialHost := ""
		if len(via) > 0 && via[0].URL != nil {
			initialHost = via[0].URL.Host
		}
		if initialHost == "" {
			initialHost = req.URL.Host
		}
		if !allowedRedirectHost(initialHost, req.URL.Host, testOrigin) {
			return errors.New("selfupdate: refusing redirect to an unauthorized host")
		}
		if testOrigin == "" && initialHost == "api.github.com" && (req.URL.Path != "/repos/ryanmccool/static-mangal/releases/latest" || req.URL.RawQuery != "") {
			return errors.New("selfupdate: refusing redirect away from the canonical release API")
		}
		return nil
	}
	return &client
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		Timeout: defaultHTTPTimeout,
	}
}

func allowedRedirectHost(initialHost, targetHost, testOrigin string) bool {
	if testOrigin != "" {
		u, err := url.Parse(testOrigin)
		return err == nil && targetHost == u.Host && initialHost == u.Host
	}
	if initialHost == "api.github.com" {
		return targetHost == "api.github.com"
	}
	return isGitHubReleaseHost(targetHost) && isGitHubReleaseHost(initialHost)
}

func isGitHubReleaseHost(host string) bool {
	switch host {
	case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
		return true
	default:
		return false
	}
}

func latestAPIURL(raw string) (string, error) {
	u, err := secureURL(raw)
	if err != nil {
		return "", fmt.Errorf("invalid release API URL: %w", err)
	}
	trimmed := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(trimmed, "/releases/latest") {
		u.Path = trimmed
		u.RawPath = ""
	} else {
		if trimmed == "" {
			u.Path = "/releases/latest"
		} else {
			u.Path = trimmed + "/releases/latest"
		}
		u.RawPath = ""
	}
	return u.String(), nil
}

func secureURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("URL must use HTTPS without credentials or fragments")
	}
	return u, nil
}

func validateDownloadURL(raw, tag, filename, testOrigin string) error {
	u, err := secureURL(raw)
	if err != nil {
		return err
	}
	expectedPath := releaseDownloadPath(tag, filename)
	if u.RawQuery != "" || u.RawPath != "" || u.Path != expectedPath || u.EscapedPath() != expectedPath {
		return errors.New("download URL does not match the expected release path")
	}
	if testOrigin != "" {
		origin, err := secureURL(testOrigin)
		if err != nil || u.Host != origin.Host {
			return errors.New("download URL is not the configured test origin")
		}
		return nil
	}
	if u.Host != "github.com" {
		return errors.New("download URL is not github.com")
	}
	return nil
}

func releaseDownloadPath(tag, filename string) string {
	return "/ryanmccool/static-mangal/releases/download/" + tag + "/" + filename
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type release struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

func Check(ctx context.Context, c Config) (Result, error) {
	return checkWith(ctx, c, testSettings{})
}

func checkWith(ctx context.Context, c Config, settings testSettings) (Result, error) {
	resolved, err := resolveConfig(c, false, settings)
	if err != nil {
		return Result{}, err
	}
	result, _, err := checkResolved(ctx, resolved)
	return result, err
}

func checkResolved(ctx context.Context, c resolvedConfig) (Result, *release, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL, nil)
	if err != nil {
		return Result{}, nil, fmt.Errorf("create release API request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "static-mangal-selfupdate")
	response, err := c.client.Do(request)
	if err != nil {
		return Result{}, nil, fmt.Errorf("get latest release: %w", err)
	}
	body, err := readResponse(response, maxDocumentSize)
	if err != nil {
		return Result{}, nil, fmt.Errorf("read latest release: %w", err)
	}
	var latest release
	if err := json.Unmarshal(body, &latest); err != nil {
		return Result{}, nil, fmt.Errorf("decode latest release: %w", err)
	}
	if latest.Draft || latest.Prerelease {
		return Result{}, nil, errors.New("selfupdate: latest release is not stable")
	}
	latestVersion, err := parseReleaseTag(latest.TagName)
	if err != nil {
		return Result{}, nil, err
	}

	result := Result{
		CurrentVersion:  c.current.String(),
		LatestVersion:   latestVersion.String(),
		UpdateAvailable: compareVersion(c.current, latestVersion) < 0,
	}
	if compareVersion(c.current, latestVersion) > 0 {
		return result, &latest, ErrLocalAhead
	}
	return result, &latest, nil
}

func readResponse(response *http.Response, limit int64) ([]byte, error) {
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	return body, nil
}

func Apply(ctx context.Context, c Config) (Result, error) {
	return applyWith(ctx, c, productionSettings())
}

func applyWith(ctx context.Context, c Config, settings testSettings) (Result, error) {
	goos := settings.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := settings.goarch
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	if goos != "darwin" && goos != "linux" {
		return Result{}, &UnsupportedPlatformError{GOOS: goos}
	}
	if !supportedArch(goarch) {
		return Result{}, fmt.Errorf("selfupdate: unsupported architecture: %s", goarch)
	}
	if c.Distribution != "archive" {
		return Result{}, fmt.Errorf("%w: %q", ErrUnsupportedDistribution, c.Distribution)
	}

	settings.goos = goos
	settings.goarch = goarch
	resolved, err := resolveConfig(c, true, settings)
	if err != nil {
		return Result{}, err
	}
	result, latest, err := checkResolved(ctx, resolved)
	if err != nil {
		return result, err
	}
	if !result.UpdateAvailable {
		return result, nil
	}

	targetInfo, err := inspectTarget(resolved.target, resolved.fs)
	if err != nil {
		return Result{}, err
	}
	archiveName := fmt.Sprintf("static-mangal_%s_%s_%s.tar.gz", result.LatestVersion, archiveOS(goos), resolved.goarch)
	archiveAsset, checksumAsset, err := selectAssets(latest, archiveName)
	if err != nil {
		return Result{}, err
	}
	if err := validateDownloadURL(archiveAsset.BrowserDownloadURL, latest.TagName, archiveName, resolved.origin); err != nil {
		return Result{}, fmt.Errorf("invalid archive download URL: %w", err)
	}
	if err := validateDownloadURL(checksumAsset.BrowserDownloadURL, latest.TagName, checksumName, resolved.origin); err != nil {
		return Result{}, fmt.Errorf("invalid checksum download URL: %w", err)
	}

	parent := filepath.Dir(resolved.target)
	lockPath, err := resolved.fs.acquireLock(parent)
	if err != nil {
		return Result{}, err
	}
	keepLock := false
	lockReleased := false
	defer func() {
		if !lockReleased && !keepLock {
			_ = resolved.fs.remove(lockPath)
		}
	}()

	targetAfterLock, err := inspectTarget(resolved.target, resolved.fs)
	if err != nil {
		return Result{}, err
	}
	if targetAfterLock.mode != targetInfo.mode || !sameFile(targetAfterLock.info, targetInfo.info) {
		return Result{}, errors.New("selfupdate: executable changed during update preparation")
	}

	checksumBody, err := downloadBody(ctx, resolved.client, checksumAsset.BrowserDownloadURL, maxDocumentSize)
	if err != nil {
		return Result{}, fmt.Errorf("download published checksums: %w", err)
	}
	expectedChecksum, err := parseChecksum(checksumBody, archiveName)
	if err != nil {
		return Result{}, err
	}

	archivePath, actualChecksum, err := downloadArchive(ctx, resolved.client, archiveAsset.BrowserDownloadURL, parent)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(archivePath)
	if subtle.ConstantTimeCompare(actualChecksum[:], expectedChecksum[:]) != 1 {
		return Result{}, ErrChecksumMismatch
	}

	candidatePath, err := stageArchive(ctx, archivePath, parent, targetInfo.mode.Perm())
	if err != nil {
		return Result{}, err
	}
	candidateOwned := true
	defer func() {
		if candidateOwned {
			_ = os.Remove(candidatePath)
		}
	}()
	candidateInfo, err := resolved.fs.lstat(candidatePath)
	if err != nil || candidateInfo.Mode()&os.ModeSymlink != 0 || !candidateInfo.Mode().IsRegular() {
		return Result{}, errors.New("selfupdate: staged candidate is not a regular file")
	}

	// Reinspect immediately before linking the hard-link backup. This closes
	// the download-to-transaction race and binds the backup to this target.
	beforeBackup, err := inspectTarget(resolved.target, resolved.fs)
	if err != nil {
		return Result{}, err
	}
	if beforeBackup.mode != targetAfterLock.mode || !sameFile(beforeBackup.info, targetAfterLock.info) {
		return Result{}, errors.New("selfupdate: executable changed during download")
	}
	backupPath := filepath.Join(parent, ".static-mangal-update-backup")
	if _, err := resolved.fs.lstat(backupPath); err == nil {
		return Result{}, errors.New("selfupdate: backup path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("check backup path: %w", err)
	}
	if err := resolved.fs.link(resolved.target, backupPath); err != nil {
		return Result{}, fmt.Errorf("create executable backup: %w", err)
	}
	backupOwned := true
	defer func() {
		if backupOwned && !keepLock {
			_ = resolved.fs.remove(backupPath)
		}
	}()
	backupInfo, err := resolved.fs.lstat(backupPath)
	if err != nil || !sameFile(backupInfo, beforeBackup.info) {
		return Result{}, errors.New("selfupdate: executable backup identity mismatch")
	}
	candidateBeforeRename, err := resolved.fs.lstat(candidatePath)
	if err != nil || !sameFile(candidateBeforeRename, candidateInfo) {
		return Result{}, errors.New("selfupdate: staged candidate identity changed")
	}
	targetBeforeRename, err := resolved.fs.lstat(resolved.target)
	if err != nil || !sameFile(targetBeforeRename, beforeBackup.info) {
		return Result{}, errors.New("selfupdate: executable changed before replacement")
	}

	if err := resolved.fs.rename(candidatePath, resolved.target); err != nil {
		return Result{}, fmt.Errorf("install staged executable: %w", err)
	}
	candidateOwned = false
	if err := verifyCandidate(resolved.fs, resolved.target, candidateInfo); err != nil {
		return rollbackResult(result, resolved, backupPath, candidateInfo, beforeBackup.info, err, &keepLock)
	}
	if err := resolved.fs.syncDir(parent); err != nil {
		return rollbackResult(result, resolved, backupPath, candidateInfo, beforeBackup.info, err, &keepLock)
	}

	validator := resolved.validate
	if validator == nil {
		validator = defaultValidate
	}
	if err := validator(ctx, resolved.target, result.LatestVersion); err != nil {
		return rollbackResult(result, resolved, backupPath, candidateInfo, beforeBackup.info, err, &keepLock)
	}
	if err := ctx.Err(); err != nil {
		return rollbackResult(result, resolved, backupPath, candidateInfo, beforeBackup.info, err, &keepLock)
	}
	if err := verifyCandidate(resolved.fs, resolved.target, candidateInfo); err != nil {
		return rollbackResult(result, resolved, backupPath, candidateInfo, beforeBackup.info, err, &keepLock)
	}

	// Removing the hard-link backup is the commit point. Before it succeeds,
	// every failure remains reversible through the direct rename rollback.
	if err := resolved.fs.remove(backupPath); err != nil {
		return rollbackResult(result, resolved, backupPath, candidateInfo, beforeBackup.info, fmt.Errorf("remove backup before commit: %w", err), &keepLock)
	}
	backupOwned = false
	if err := resolved.fs.syncDir(parent); err != nil {
		keepLock = true
		return result, &CommittedUpdateError{WarningErr: fmt.Errorf("sync committed update: %w", err)}
	}
	if err := resolved.fs.remove(lockPath); err != nil {
		keepLock = true
		return result, &CommittedUpdateError{WarningErr: fmt.Errorf("remove lock after commit: %w", err)}
	}
	lockReleased = true
	if err := resolved.fs.syncDir(parent); err != nil {
		return result, &CommittedUpdateError{WarningErr: fmt.Errorf("sync after lock removal: %w", err)}
	}
	return result, nil
}

func archiveOS(goos string) string {
	if goos == "darwin" {
		return "Darwin"
	}
	return "Linux"
}

func selectAssets(latest *release, archiveName string) (releaseAsset, releaseAsset, error) {
	var archiveAsset, checksumAsset releaseAsset
	archiveCount, checksumCount := 0, 0
	for _, asset := range latest.Assets {
		switch asset.Name {
		case archiveName:
			archiveCount++
			archiveAsset = asset
		case checksumName:
			checksumCount++
			checksumAsset = asset
		}
	}
	if archiveCount != 1 {
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("selfupdate: expected exactly one %q asset, found %d", archiveName, archiveCount)
	}
	if checksumCount != 1 {
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("selfupdate: expected exactly one %q asset, found %d", checksumName, checksumCount)
	}
	if archiveAsset.BrowserDownloadURL == "" || checksumAsset.BrowserDownloadURL == "" {
		return releaseAsset{}, releaseAsset{}, errors.New("selfupdate: release asset has no download URL")
	}
	return archiveAsset, checksumAsset, nil
}

func downloadBody(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "static-mangal-selfupdate")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	return readResponse(response, limit)
}

func downloadArchive(ctx context.Context, client *http.Client, rawURL, parent string) (string, [32]byte, error) {
	var empty [32]byte
	f, err := os.CreateTemp(parent, ".static-mangal-update-archive-*")
	if err != nil {
		return "", empty, fmt.Errorf("create archive staging file: %w", err)
	}
	archivePath := f.Name()
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = os.Remove(archivePath)
		}
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		_ = f.Close()
		return "", empty, err
	}
	request.Header.Set("User-Agent", "static-mangal-selfupdate")
	response, err := client.Do(request)
	if err != nil {
		_ = f.Close()
		return "", empty, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_ = f.Close()
		return "", empty, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	if response.ContentLength > maxArchiveSize {
		_ = f.Close()
		return "", empty, fmt.Errorf("archive exceeds %d-byte limit", maxArchiveSize)
	}

	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hasher), io.LimitReader(response.Body, maxArchiveSize+1))
	if err != nil {
		_ = f.Close()
		return "", empty, fmt.Errorf("download archive: %w", err)
	}
	if n > maxArchiveSize {
		_ = f.Close()
		return "", empty, fmt.Errorf("archive exceeds %d-byte limit", maxArchiveSize)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return "", empty, fmt.Errorf("sync downloaded archive: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", empty, fmt.Errorf("close downloaded archive: %w", err)
	}
	var sum [32]byte
	copy(sum[:], hasher.Sum(nil))
	removeOnError = false
	return archivePath, sum, nil
}

func parseChecksum(body []byte, archiveName string) ([32]byte, error) {
	var checksum [32]byte
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	found := 0
	for i, line := range lines {
		if line == "" && i == len(lines)-1 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 || fields[1] == "" {
			return checksum, fmt.Errorf("selfupdate: malformed checksum line %d", i+1)
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return checksum, fmt.Errorf("selfupdate: malformed checksum line %d", i+1)
		}
		if fields[1] != archiveName {
			continue
		}
		found++
		if found > 1 {
			return checksum, fmt.Errorf("selfupdate: duplicate checksum for %q", archiveName)
		}
		copy(checksum[:], decoded)
	}
	if found != 1 {
		return checksum, fmt.Errorf("selfupdate: checksum for %q is missing", archiveName)
	}
	return checksum, nil
}

type targetInfo struct {
	info       os.FileInfo
	parentInfo os.FileInfo
	mode       os.FileMode
}

func inspectTarget(target string, fs transactionFS) (targetInfo, error) {
	if target == "" {
		return targetInfo{}, errors.New("selfupdate: executable path is empty")
	}
	info, err := fs.lstat(target)
	if err != nil {
		return targetInfo{}, fmt.Errorf("inspect executable: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return targetInfo{}, errors.New("selfupdate: executable target must be a regular non-symlink file")
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return targetInfo{}, errors.New("selfupdate: refusing setuid or setgid executable target")
	}
	if info.Mode().Perm()&0100 == 0 {
		return targetInfo{}, errors.New("selfupdate: executable target has no owner-execute permission")
	}
	for ancestor := target; ; ancestor = filepath.Dir(ancestor) {
		ancestorInfo, ancestorErr := fs.lstat(ancestor)
		if ancestorErr != nil {
			return targetInfo{}, fmt.Errorf("inspect executable ancestor: %w", ancestorErr)
		}
		if ancestor != target && ancestorInfo.Mode()&os.ModeSymlink != 0 {
			return targetInfo{}, errors.New("selfupdate: executable target has a symlinked ancestor")
		}
		next := filepath.Dir(ancestor)
		if next == ancestor {
			break
		}
	}
	parent := filepath.Dir(target)
	parentInfo, err := fs.lstat(parent)
	if err != nil {
		return targetInfo{}, fmt.Errorf("inspect executable parent: %w", err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return targetInfo{}, errors.New("selfupdate: executable parent must be a real directory")
	}
	if parentInfo.Mode().Perm()&0022 != 0 {
		return targetInfo{}, errors.New("selfupdate: executable parent is group- or world-writable")
	}
	if !ownedByEffectiveUser(info) || !ownedByEffectiveUser(parentInfo) {
		return targetInfo{}, errors.New("selfupdate: executable and parent must be owned by the effective user")
	}
	return targetInfo{info: info, parentInfo: parentInfo, mode: info.Mode()}, nil
}

func sameFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b)
}

func createLock(parent string) (string, error) {
	lockPath := filepath.Join(parent, ".static-mangal-update.lock")
	f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", ErrLockExists
		}
		return "", fmt.Errorf("create update lock: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(lockPath)
		return "", fmt.Errorf("sync update lock: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(lockPath)
		return "", fmt.Errorf("close update lock: %w", err)
	}
	return lockPath, nil
}

func stageArchive(ctx context.Context, archivePath, parent string, mode os.FileMode) (string, error) {
	output, err := os.CreateTemp(parent, ".static-mangal-update-candidate-*")
	if err != nil {
		return "", fmt.Errorf("create candidate: %w", err)
	}
	candidatePath := output.Name()
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = os.Remove(candidatePath)
		}
	}()

	if err := extractArchive(ctx, archivePath, output); err != nil {
		_ = output.Close()
		return "", err
	}
	if err := output.Chmod(mode); err != nil {
		_ = output.Close()
		return "", fmt.Errorf("set candidate permissions: %w", err)
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return "", fmt.Errorf("sync candidate: %w", err)
	}
	if err := output.Close(); err != nil {
		return "", fmt.Errorf("close candidate: %w", err)
	}
	removeOnError = false
	return candidatePath, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.r.Read(p)
	}
}

type zeroOnlyWriter struct {
	limit int64
	n     int64
}

func (w *zeroOnlyWriter) Write(p []byte) (int, error) {
	if w.n+int64(len(p)) > w.limit {
		return 0, errors.New("selfupdate: archive trailing data exceeds limit")
	}
	for _, b := range p {
		if b != 0 {
			return 0, errors.New("selfupdate: archive has trailing non-padding data")
		}
	}
	w.n += int64(len(p))
	return len(p), nil
}

type decompressedLimitReader struct {
	r         io.Reader
	remaining int64
}

func (r *decompressedLimitReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, errors.New("selfupdate: decompressed archive exceeds limit")
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func extractArchive(ctx context.Context, archivePath string, output io.Writer) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open downloaded archive: %w", err)
	}
	defer archive.Close()

	source := bufio.NewReader(contextReader{ctx: ctx, r: archive})
	gzipReader, err := gzip.NewReader(source)
	if err != nil {
		return fmt.Errorf("open archive gzip stream: %w", err)
	}
	gzipReader.Multistream(false)
	defer gzipReader.Close()
	limited := &decompressedLimitReader{r: gzipReader, remaining: maxArchiveSize}
	tarReader := tar.NewReader(limited)
	found := false
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("read archive entry: %w", nextErr)
		}
		if err := validateArchiveEntryName(header.Name); err != nil {
			return err
		}
		if header.Size < 0 || header.Size > maxArchiveSize {
			return fmt.Errorf("selfupdate: archive entry %q exceeds limit", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if header.Size != 0 {
				return fmt.Errorf("selfupdate: directory entry %q has content", header.Name)
			}
			continue
		case tar.TypeReg, tar.TypeRegA:
			if header.Name == binaryName {
				if found {
					return errors.New("selfupdate: archive contains duplicate static-mangal executables")
				}
				found = true
				if _, err := io.CopyN(output, tarReader, header.Size); err != nil {
					return fmt.Errorf("read archive binary: %w", err)
				}
			} else if _, err := io.CopyN(io.Discard, tarReader, header.Size); err != nil {
				return fmt.Errorf("read archive support file %q: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("selfupdate: archive entry %q is an unsafe link or special file", header.Name)
		}
	}
	if !found {
		return errors.New("selfupdate: archive does not contain static-mangal")
	}

	trailer := &zeroOnlyWriter{limit: maxArchiveSize}
	if _, err := io.Copy(trailer, limited); err != nil {
		return fmt.Errorf("validate archive trailer: %w", err)
	}
	if _, err := source.ReadByte(); err != io.EOF {
		if err == nil {
			return errors.New("selfupdate: archive contains trailing compressed data")
		}
		return fmt.Errorf("read archive end: %w", err)
	}
	return nil
}

func validateArchiveEntryName(name string) error {
	if name == "" || path.IsAbs(name) || strings.HasPrefix(name, `\`) || strings.ContainsRune(name, 0) {
		return errors.New("selfupdate: archive contains an unsafe path")
	}
	if strings.Contains(name, `\`) || strings.Contains(name, "//") {
		return errors.New("selfupdate: archive contains an unsafe path")
	}
	core := strings.TrimSuffix(name, "/")
	for _, component := range strings.Split(core, "/") {
		if component == "" || component == "." || component == ".." {
			return errors.New("selfupdate: archive contains a traversal path")
		}
	}
	return nil
}

func defaultValidate(ctx context.Context, target, expectedVersion string) error {
	validationCtx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()
	command := exec.CommandContext(validationCtx, target, "version", "--short")
	stdout := &limitedBuffer{limit: validationOutput}
	command.Stdout = stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return fmt.Errorf("validate installed executable: %w", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != expectedVersion {
		return fmt.Errorf("validate installed executable: got version %q, want %q", got, expectedVersion)
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("selfupdate: validation output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func verifyCandidate(fs transactionFS, target string, candidate os.FileInfo) error {
	current, err := fs.lstat(target)
	if err != nil {
		return fmt.Errorf("verify replacement identity: %w", err)
	}
	if current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || !sameFile(current, candidate) {
		return errors.New("selfupdate: replacement target identity changed")
	}
	return nil
}

func rollbackResult(result Result, resolved resolvedConfig, backup string, candidate, original os.FileInfo, updateErr error, keepLock *bool) (Result, error) {
	if err := rollback(resolved.fs, resolved.target, backup, filepath.Dir(resolved.target), candidate, original); err != nil {
		*keepLock = true
		return result, &RollbackError{UpdateErr: updateErr, RollbackErr: err}
	}
	return result, updateErr
}

func rollback(fs transactionFS, target, backup, parent string, candidate, original os.FileInfo) error {
	current, err := fs.lstat(target)
	if err != nil {
		return fmt.Errorf("rollback refused: inspect replacement: %w", err)
	}
	if current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || !sameFile(current, candidate) {
		return errors.New("rollback refused: replacement target identity changed")
	}
	backupInfo, err := fs.lstat(backup)
	if err != nil {
		return fmt.Errorf("inspect executable backup: %w", err)
	}
	if backupInfo.Mode()&os.ModeSymlink != 0 || !backupInfo.Mode().IsRegular() {
		return errors.New("selfupdate: executable backup is not a regular non-symlink file")
	}
	if !sameFile(backupInfo, original) {
		return errors.New("rollback refused: executable backup identity changed")
	}
	if err := fs.rename(backup, target); err != nil {
		return fmt.Errorf("restore executable backup: %w", err)
	}
	restored, err := fs.lstat(target)
	if err != nil || !sameFile(restored, backupInfo) {
		if err != nil {
			return fmt.Errorf("verify restored executable: %w", err)
		}
		return errors.New("verify restored executable: identity mismatch")
	}
	return fs.syncDir(parent)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil && !directorySyncUnsupported(syncErr) {
		return fmt.Errorf("sync directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close directory after sync: %w", closeErr)
	}
	return nil
}

func directorySyncUnsupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.EBADF)
}
