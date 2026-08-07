package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type testAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type testReleaseState struct {
	tag                   string
	draft                 bool
	prerelease            bool
	releaseStatus         int
	releaseBody           []byte
	assets                []testAsset
	archive               []byte
	archiveStatus         int
	archiveContentLength  int64
	archiveRedirect       string
	onArchive             func()
	checksum              []byte
	checksumStatus        int
	checksumContentLength int64
}

func newTestReleaseServer(t *testing.T, state *testReleaseState) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			status := state.releaseStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			if state.releaseBody != nil {
				_, _ = w.Write(state.releaseBody)
				return
			}
			assets := state.assets
			if assets == nil {
				assets = []testAsset{
					{Name: archiveName(state.tag, "Darwin", "amd64"), URL: server.URL + releaseDownloadPath(state.tag, archiveName(state.tag, "Darwin", "amd64"))},
					{Name: checksumName, URL: server.URL + releaseDownloadPath(state.tag, checksumName)},
				}
			} else {
				assets = append([]testAsset(nil), assets...)
				for i := range assets {
					if assets[i].URL == "" {
						if assets[i].Name == checksumName {
							assets[i].URL = server.URL + releaseDownloadPath(state.tag, checksumName)
						} else {
							assets[i].URL = server.URL + releaseDownloadPath(state.tag, assets[i].Name)
						}
					} else if strings.HasPrefix(assets[i].URL, server.URL+"/") {
						assets[i].URL = server.URL + releaseDownloadPath(state.tag, assets[i].Name)
					}
				}
			}
			payload := struct {
				TagName    string      `json:"tag_name"`
				Draft      bool        `json:"draft"`
				Prerelease bool        `json:"prerelease"`
				Assets     []testAsset `json:"assets"`
			}{state.tag, state.draft, state.prerelease, assets}
			_ = json.NewEncoder(w).Encode(payload)
		case releaseDownloadPath(state.tag, archiveName(state.tag, "Darwin", "amd64")):
			if state.onArchive != nil {
				state.onArchive()
			}
			if state.archiveRedirect != "" {
				http.Redirect(w, r, state.archiveRedirect, http.StatusFound)
				return
			}
			writeTestBody(w, state.archiveStatus, state.archiveContentLength, state.archive)
		case releaseDownloadPath(state.tag, checksumName):
			writeTestBody(w, state.checksumStatus, state.checksumContentLength, state.checksum)
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func writeTestBody(w http.ResponseWriter, status int, contentLength int64, body []byte) {
	if status == 0 {
		status = http.StatusOK
	}
	if contentLength > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", contentLength))
	}
	w.WriteHeader(status)
	if body != nil {
		_, _ = w.Write(body)
	}
}

func archiveName(version, goos, arch string) string {
	return fmt.Sprintf("static-mangal_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), goos, arch)
}

func makeArchive(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{
			Name:     entry.name,
			Mode:     0755,
			Size:     int64(len(entry.data)),
			Typeflag: entry.typeflag,
			Linkname: entry.linkname,
		}
		if entry.size >= 0 {
			header.Size = entry.size
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if len(entry.data) > 0 {
			if _, err := tarWriter.Write(entry.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func makeOversizedArchive(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: binaryName, Mode: 0755, Size: maxArchiveSize + 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

type archiveEntry struct {
	name     string
	typeflag byte
	linkname string
	data     []byte
	size     int64
}

func regularEntry(name string, data []byte) archiveEntry {
	return archiveEntry{name: name, typeflag: tar.TypeReg, data: data, size: -1}
}

func releaseStateForArchive(t *testing.T, tag, goos, arch string, archive []byte) *testReleaseState {
	t.Helper()
	name := archiveName(tag, goos, arch)
	sum := sha256.Sum256(archive)
	return &testReleaseState{
		tag:      tag,
		archive:  archive,
		checksum: []byte(fmt.Sprintf("%x  %s\n", sum, name)),
	}
}

func updateConfig(server *httptest.Server, target, current, goos, arch string) Config {
	return Config{
		CurrentVersion: current,
		Distribution:   "archive",
		Validate: func(ctx context.Context, path, expected string) error {
			return nil
		},
	}
}

func updateSettings(server *httptest.Server, target, goos, arch string) testSettings {
	return testSettings{
		apiURL: server.URL + "/releases/latest",
		client: server.Client(),
		goos:   goos,
		goarch: arch,
		target: target,
		origin: server.URL,
	}
}

func newTarget(t *testing.T, content string, mode os.FileMode) (string, string) {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, binaryName)
	if err := os.WriteFile(target, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return directory, target
}

func targetContent(t *testing.T, target string) string {
	t.Helper()
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCheckSemverOrderingAndExactReleaseTag(t *testing.T) {
	cases := []struct {
		name    string
		current string
		latest  string
		update  bool
		ahead   bool
	}{
		{name: "equal", current: "1.2.3", latest: "v1.2.3"},
		{name: "patch newer", current: "1.2.3", latest: "v1.2.4", update: true},
		{name: "minor newer", current: "1.9.9", latest: "v1.10.0", update: true},
		{name: "major local ahead", current: "2.0.0", latest: "v1.99.99", ahead: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newTestReleaseServer(t, &testReleaseState{tag: tc.latest})
			defer server.Close()
			result, err := checkWith(context.Background(), Config{
				CurrentVersion: tc.current,
			}, testSettings{apiURL: server.URL, client: server.Client(), origin: server.URL})
			if tc.ahead {
				if !errors.Is(err, ErrLocalAhead) {
					t.Fatalf("expected local-ahead error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.CurrentVersion != tc.current || result.LatestVersion != strings.TrimPrefix(tc.latest, "v") || result.UpdateAvailable != tc.update {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestCheckRejectsUnstableMalformedAndUnsuccessfulReleases(t *testing.T) {
	cases := []struct {
		name  string
		state testReleaseState
	}{
		{name: "draft", state: testReleaseState{tag: "v1.2.3", draft: true}},
		{name: "prerelease", state: testReleaseState{tag: "v1.2.3", prerelease: true}},
		{name: "malformed tag", state: testReleaseState{tag: "1.2.3"}},
		{name: "double v tag", state: testReleaseState{tag: "vv1.2.3"}},
		{name: "server error", state: testReleaseState{tag: "v1.2.3", releaseStatus: http.StatusBadGateway}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newTestReleaseServer(t, &tc.state)
			defer server.Close()
			_, err := checkWith(context.Background(), Config{
				CurrentVersion: "1.0.0",
			}, testSettings{apiURL: server.URL + "/releases/latest", client: server.Client(), origin: server.URL})
			if err == nil {
				t.Fatal("expected release error")
			}
		})
	}
}

func TestCheckRejectsOversizedReleaseBody(t *testing.T) {
	server := newTestReleaseServer(t, &testReleaseState{
		releaseBody: bytes.Repeat([]byte("x"), maxDocumentSize+1),
	})
	defer server.Close()
	_, err := checkWith(context.Background(), Config{
		CurrentVersion: "1.0.0",
	}, testSettings{apiURL: server.URL + "/releases/latest", client: server.Client(), origin: server.URL})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected bounded response error, got %v", err)
	}
}

func TestApplySuccessUsesExactAssetsAndPreservesExecutablePermissions(t *testing.T) {
	directory, target := newTarget(t, "old", 0750)
	archive := makeArchive(t,
		archiveEntry{name: "completions/", typeflag: tar.TypeDir, size: 0},
		regularEntry("README.md", []byte("readme")),
		regularEntry(binaryName, []byte("new")),
		regularEntry("LICENSE", []byte("license")),
	)
	state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
	server := newTestReleaseServer(t, state)
	defer server.Close()
	state.assets = []testAsset{
		{Name: "static-mangal_1.2.3_Darwin_amd64.tar.gz.old", URL: server.URL + "/archive"},
		{Name: archiveName("v1.2.3", "Darwin", "amd64"), URL: server.URL + "/archive"},
		{Name: checksumName, URL: server.URL + "/checksums"},
	}
	called := false
	config := updateConfig(server, target, "1.0.0", "darwin", "amd64")
	config.Validate = func(ctx context.Context, path, expected string) error {
		called = true
		if path != target || expected != "1.2.3" || targetContent(t, path) != "new" {
			return errors.New("unexpected validation input")
		}
		return nil
	}
	result, err := applyWith(context.Background(), config, updateSettings(server, target, "darwin", "amd64"))
	if err != nil {
		t.Fatal(err)
	}
	if !called || !result.UpdateAvailable || result.LatestVersion != "1.2.3" {
		t.Fatalf("unexpected success result: %+v, validation=%v", result, called)
	}
	if got := targetContent(t, target); got != "new" {
		t.Fatalf("target content = %q, want new", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0750); got != want {
		t.Fatalf("target mode = %o, want %o", got, want)
	}
	for _, name := range []string{".static-mangal-update.lock", ".static-mangal-update-backup"} {
		if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected transaction artifact %s: %v", name, err)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(directory, ".static-mangal-update-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("unexpected temporary files: %v", leftovers)
	}
}

func TestApplyRejectsOffOriginAssetAndRedirect(t *testing.T) {
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer other.Close()

	_, target := newTarget(t, "old", 0755)
	state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
	server := newTestReleaseServer(t, state)
	defer server.Close()
	archiveURL := other.URL + releaseDownloadPath(state.tag, archiveName(state.tag, "Darwin", "amd64"))
	state.assets = []testAsset{
		{Name: archiveName(state.tag, "Darwin", "amd64"), URL: archiveURL},
		{Name: checksumName, URL: server.URL + "/checksums"},
	}
	_, err := applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), updateSettings(server, target, "darwin", "amd64"))
	if err == nil || targetContent(t, target) != "old" {
		t.Fatalf("expected off-origin asset refusal, got %v", err)
	}

	state.assets = nil
	state.archiveRedirect = other.URL + releaseDownloadPath(state.tag, archiveName(state.tag, "Darwin", "amd64"))
	_, err = applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), updateSettings(server, target, "darwin", "amd64"))
	if err == nil || !strings.Contains(err.Error(), "unauthorized host") || targetContent(t, target) != "old" {
		t.Fatalf("expected off-origin redirect refusal, got %v", err)
	}
}

func TestApplyPrivatePlatformAndArchitectureBoundaries(t *testing.T) {
	config := Config{CurrentVersion: "1.0.0", Distribution: "archive"}
	if _, err := applyWith(context.Background(), config, testSettings{goos: "windows", goarch: "amd64"}); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("expected Windows platform refusal, got %v", err)
	}
	if _, err := applyWith(context.Background(), config, testSettings{goos: "linux", goarch: "mips64"}); err == nil || !strings.Contains(err.Error(), "unsupported architecture") {
		t.Fatalf("expected architecture refusal, got %v", err)
	}
}

func TestApplyRejectsAncestorAndInvocationSymlinks(t *testing.T) {
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
	server := newTestReleaseServer(t, state)
	defer server.Close()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realDir := filepath.Join(base, "real")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(base, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}
	realTarget := filepath.Join(realDir, binaryName)
	if err := os.WriteFile(realTarget, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	linkedTarget := filepath.Join(linkDir, binaryName)
	_, err = applyWith(context.Background(), updateConfig(server, linkedTarget, "1.0.0", "darwin", "amd64"), updateSettings(server, linkedTarget, "darwin", "amd64"))
	if err == nil || targetContent(t, realTarget) != "old" {
		t.Fatalf("expected ancestor symlink refusal, got %v", err)
	}

	invocation := filepath.Join(base, "invocation")
	if err := os.Symlink(realTarget, invocation); err != nil {
		t.Fatal(err)
	}
	settings := updateSettings(server, "", "darwin", "amd64")
	settings.executable = func() (string, error) { return invocation, nil }
	_, err = applyWith(context.Background(), updateConfig(server, invocation, "1.0.0", "darwin", "amd64"), settings)
	if err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("expected invocation symlink refusal, got %v", err)
	}
}

func TestApplyRejectsNonExecutableAndLocalAhead(t *testing.T) {
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
	server := newTestReleaseServer(t, state)
	defer server.Close()
	_, target := newTarget(t, "old", 0644)
	_, err := applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), updateSettings(server, target, "darwin", "amd64"))
	if err == nil || !strings.Contains(err.Error(), "owner-execute") || targetContent(t, target) != "old" {
		t.Fatalf("expected non-executable refusal, got %v", err)
	}
	_, target = newTarget(t, "old", 0401)
	_, err = applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), updateSettings(server, target, "darwin", "amd64"))
	if err == nil || !strings.Contains(err.Error(), "owner-execute") || targetContent(t, target) != "old" {
		t.Fatalf("expected owner-execute refusal, got %v", err)
	}

	_, target = newTarget(t, "old", 0755)
	result, err := applyWith(context.Background(), updateConfig(server, target, "2.0.0", "darwin", "amd64"), updateSettings(server, target, "darwin", "amd64"))
	if !errors.Is(err, ErrLocalAhead) || result.LatestVersion != "1.2.3" || targetContent(t, target) != "old" {
		t.Fatalf("expected local-ahead refusal, result=%+v err=%v", result, err)
	}
}

func TestApplyRejectsTargetMutationDuringDownloadAndValidation(t *testing.T) {
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
	server := newTestReleaseServer(t, state)
	defer server.Close()
	_, target := newTarget(t, "old", 0755)
	state.onArchive = func() {
		moved := target + ".external"
		_ = os.Rename(target, moved)
		_ = os.WriteFile(target, []byte("external"), 0755)
	}
	_, err := applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), updateSettings(server, target, "darwin", "amd64"))
	if err == nil || targetContent(t, target) != "external" {
		t.Fatalf("expected download-time mutation refusal, got %v", err)
	}

	state.onArchive = nil
	_, target = newTarget(t, "old", 0755)
	config := updateConfig(server, target, "1.0.0", "darwin", "amd64")
	config.Validate = func(ctx context.Context, path, expected string) error {
		moved := path + ".validation-external"
		if err := os.Rename(path, moved); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("external"), 0755)
	}
	_, err = applyWith(context.Background(), config, updateSettings(server, target, "darwin", "amd64"))
	var rollbackErr *RollbackError
	if !errors.As(err, &rollbackErr) || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("expected identity-preserving rollback refusal, got %v", err)
	}
	if targetContent(t, target) != "external" {
		t.Fatal("validation mutation was overwritten")
	}
	for _, name := range []string{".static-mangal-update.lock", ".static-mangal-update-backup"} {
		if _, statErr := os.Lstat(filepath.Join(filepath.Dir(target), name)); statErr != nil {
			t.Fatalf("expected preserved transaction artifact %s: %v", name, statErr)
		}
	}
}

func TestTransactionFilesystemFailureSeam(t *testing.T) {
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
	server := newTestReleaseServer(t, state)
	defer server.Close()

	t.Run("initial install rename failure", func(t *testing.T) {
		_, target := newTarget(t, "old", 0755)
		fs := productionTransactionFS()
		fs.rename = func(old, new string) error { return errors.New("install rename failure") }
		settings := updateSettings(server, target, "darwin", "amd64")
		settings.fs = fs
		_, err := applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), settings)
		if err == nil || !strings.Contains(err.Error(), "install staged executable") || targetContent(t, target) != "old" {
			t.Fatalf("expected install rename failure, got %v", err)
		}
		for _, name := range []string{".static-mangal-update.lock", ".static-mangal-update-backup"} {
			if _, statErr := os.Lstat(filepath.Join(filepath.Dir(target), name)); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("unexpected preserved %s: %v", name, statErr)
			}
		}
	})

	t.Run("precommit directory sync rolls back", func(t *testing.T) {
		_, target := newTarget(t, "old", 0755)
		fs := productionTransactionFS()
		syncCalls := 0
		fs.syncDir = func(path string) error {
			syncCalls++
			if syncCalls == 1 {
				return errors.New("precommit sync failure")
			}
			return syncDirectory(path)
		}
		settings := updateSettings(server, target, "darwin", "amd64")
		settings.fs = fs
		_, err := applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), settings)
		if err == nil || !strings.Contains(err.Error(), "precommit sync failure") || targetContent(t, target) != "old" {
			t.Fatalf("expected precommit rollback, got %v", err)
		}
	})

	t.Run("rollback sync failure retains recovery state", func(t *testing.T) {
		_, target := newTarget(t, "old", 0755)
		config := updateConfig(server, target, "1.0.0", "darwin", "amd64")
		config.Validate = func(context.Context, string, string) error { return errors.New("validation failure") }
		fs := productionTransactionFS()
		syncCalls := 0
		fs.syncDir = func(path string) error {
			syncCalls++
			if syncCalls == 2 {
				return errors.New("rollback sync failure")
			}
			return syncDirectory(path)
		}
		settings := updateSettings(server, target, "darwin", "amd64")
		settings.fs = fs
		_, err := applyWith(context.Background(), config, settings)
		var rollbackErr *RollbackError
		if !errors.As(err, &rollbackErr) || !strings.Contains(err.Error(), "rollback sync failure") {
			t.Fatalf("expected rollback sync failure, got %v", err)
		}
		if _, statErr := os.Lstat(filepath.Join(filepath.Dir(target), ".static-mangal-update.lock")); statErr != nil {
			t.Fatalf("expected recovery lock: %v", statErr)
		}
	})

	t.Run("backup cleanup rolls back", func(t *testing.T) {
		_, target := newTarget(t, "old", 0755)
		config := updateConfig(server, target, "1.0.0", "darwin", "amd64")
		fs := productionTransactionFS()
		fs.remove = func(name string) error {
			if filepath.Base(name) == ".static-mangal-update-backup" {
				return errors.New("backup cleanup failure")
			}
			return os.Remove(name)
		}
		settings := updateSettings(server, target, "darwin", "amd64")
		settings.fs = fs
		_, err := applyWith(context.Background(), config, settings)
		if err == nil || !strings.Contains(err.Error(), "backup before commit") || targetContent(t, target) != "old" {
			t.Fatalf("expected uncommitted rollback, got %v", err)
		}
	})

	t.Run("rollback failure preserves state", func(t *testing.T) {
		_, target := newTarget(t, "old", 0755)
		config := updateConfig(server, target, "1.0.0", "darwin", "amd64")
		config.Validate = func(context.Context, string, string) error { return errors.New("validation failure") }
		fs := productionTransactionFS()
		fs.rename = func(old, new string) error {
			if filepath.Base(old) == ".static-mangal-update-backup" {
				return errors.New("rollback rename failure")
			}
			return os.Rename(old, new)
		}
		settings := updateSettings(server, target, "darwin", "amd64")
		settings.fs = fs
		_, err := applyWith(context.Background(), config, settings)
		var rollbackErr *RollbackError
		if !errors.As(err, &rollbackErr) || !strings.Contains(err.Error(), "rollback rename failure") {
			t.Fatalf("expected rollback failure, got %v", err)
		}
		for _, name := range []string{".static-mangal-update.lock", ".static-mangal-update-backup"} {
			if _, statErr := os.Lstat(filepath.Join(filepath.Dir(target), name)); statErr != nil {
				t.Fatalf("expected preserved %s: %v", name, statErr)
			}
		}
	})

	t.Run("committed cleanup warning", func(t *testing.T) {
		_, target := newTarget(t, "old", 0755)
		fs := productionTransactionFS()
		fs.remove = func(name string) error {
			if filepath.Base(name) == ".static-mangal-update.lock" {
				return errors.New("lock cleanup failure")
			}
			return os.Remove(name)
		}
		settings := updateSettings(server, target, "darwin", "amd64")
		settings.fs = fs
		_, err := applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), settings)
		var committedErr *CommittedUpdateError
		if !errors.As(err, &committedErr) || targetContent(t, target) != "new" {
			t.Fatalf("expected committed warning, got %v", err)
		}
		if _, statErr := os.Lstat(filepath.Join(filepath.Dir(target), ".static-mangal-update-backup")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("backup remains after commit: %v", statErr)
		}
	})

	t.Run("postcommit directory sync warning", func(t *testing.T) {
		_, target := newTarget(t, "old", 0755)
		fs := productionTransactionFS()
		syncCalls := 0
		fs.syncDir = func(path string) error {
			syncCalls++
			if syncCalls == 2 {
				return errors.New("postcommit sync failure")
			}
			return syncDirectory(path)
		}
		settings := updateSettings(server, target, "darwin", "amd64")
		settings.fs = fs
		_, err := applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), settings)
		var committedErr *CommittedUpdateError
		if !errors.As(err, &committedErr) || !strings.Contains(err.Error(), "postcommit sync failure") || targetContent(t, target) != "new" {
			t.Fatalf("expected committed postcommit warning, got %v", err)
		}
		if _, statErr := os.Lstat(filepath.Join(filepath.Dir(target), ".static-mangal-update-backup")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("backup remains after commit: %v", statErr)
		}
	})
}

func TestOwnershipGuardWhereSupported(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix ownership test")
	}
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
	server := newTestReleaseServer(t, state)
	defer server.Close()
	_, target := newTarget(t, "old", 0755)
	uid := os.Geteuid() + 1
	if err := os.Chown(target, uid, -1); err != nil {
		t.Skipf("ownership mutation unavailable: %v", err)
	}
	_, err := applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), updateSettings(server, target, "darwin", "amd64"))
	if err == nil || !strings.Contains(err.Error(), "owned by") {
		t.Fatalf("expected ownership refusal, got %v", err)
	}
}

func TestApplyErrorsLeaveTargetUntouched(t *testing.T) {
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	sum := sha256.Sum256(archive)
	archiveFile := archiveName("v1.2.3", "Darwin", "amd64")
	validChecksum := []byte(fmt.Sprintf("%x  %s\n", sum, archiveFile))
	wrongChecksum := sha256.Sum256([]byte("wrong"))
	cases := []struct {
		name  string
		state func(*testReleaseState)
	}{
		{name: "malformed checksum", state: func(s *testReleaseState) { s.checksum = []byte("bad checksum\n") }},
		{name: "missing checksum", state: func(s *testReleaseState) { s.checksum = []byte(fmt.Sprintf("%x  other.tar.gz\n", sum)) }},
		{name: "duplicate checksum", state: func(s *testReleaseState) { s.checksum = append(validChecksum, validChecksum...) }},
		{name: "checksum mismatch", state: func(s *testReleaseState) { s.checksum = []byte(fmt.Sprintf("%x  %s\n", wrongChecksum, archiveFile)) }},
		{name: "archive status", state: func(s *testReleaseState) { s.archiveStatus = http.StatusBadGateway }},
		{name: "duplicate archive asset", state: func(s *testReleaseState) {
			s.assets = []testAsset{
				{Name: archiveFile, URL: ""},
				{Name: archiveFile, URL: ""},
				{Name: checksumName, URL: ""},
			}
		}},
		{name: "duplicate checksum asset", state: func(s *testReleaseState) {
			s.assets = []testAsset{
				{Name: archiveFile, URL: ""},
				{Name: checksumName, URL: ""},
				{Name: checksumName, URL: ""},
			}
		}},
		{name: "HTTP archive URL", state: func(s *testReleaseState) {
			s.assets = []testAsset{
				{Name: archiveFile, URL: "http://example.invalid/archive"},
				{Name: checksumName, URL: ""},
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			directory, target := newTarget(t, "old", 0755)
			state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
			state.checksum = validChecksum
			server := newTestReleaseServer(t, state)
			defer server.Close()
			tc.state(state)
			config := updateConfig(server, target, "1.0.0", "darwin", "amd64")
			_, err := applyWith(context.Background(), config, updateSettings(server, target, "darwin", "amd64"))
			if err == nil {
				t.Fatal("expected update error")
			}
			if got := targetContent(t, target); got != "old" {
				t.Fatalf("target changed to %q after error", got)
			}
			if _, err := os.Lstat(filepath.Join(directory, ".static-mangal-update.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("lock cleanup error: %v", err)
			}
		})
	}
}

func TestApplyRejectsOversizedChecksumAndArchive(t *testing.T) {
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	cases := []struct {
		name  string
		state func(*testReleaseState)
	}{
		{name: "checksum body", state: func(s *testReleaseState) {
			s.checksum = bytes.Repeat([]byte("x"), maxDocumentSize+1)
		}},
		{name: "archive body", state: func(s *testReleaseState) {
			s.archiveContentLength = maxArchiveSize + 1
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			directory, target := newTarget(t, "old", 0755)
			state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
			server := newTestReleaseServer(t, state)
			defer server.Close()
			tc.state(state)
			config := updateConfig(server, target, "1.0.0", "darwin", "amd64")
			_, err := applyWith(context.Background(), config, updateSettings(server, target, "darwin", "amd64"))
			if err == nil || targetContent(t, target) != "old" {
				t.Fatalf("expected bounded error with unchanged target, got %v", err)
			}
			if _, err := os.Lstat(filepath.Join(directory, ".static-mangal-update.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("lock cleanup error: %v", err)
			}
		})
	}
}

func TestApplyRejectsMaliciousTarEntriesAndTruncation(t *testing.T) {
	validData := []byte("new")
	validArchive := makeArchive(t, regularEntry(binaryName, validData))
	cases := []struct {
		name    string
		archive []byte
	}{
		{name: "nested path", archive: makeArchive(t, regularEntry("nested/static-mangal", validData))},
		{name: "traversal path", archive: makeArchive(t, regularEntry("../static-mangal", validData))},
		{name: "absolute path", archive: makeArchive(t, regularEntry("/static-mangal", validData))},
		{name: "directory", archive: makeArchive(t, archiveEntry{name: binaryName, typeflag: tar.TypeDir, size: 0})},
		{name: "symlink", archive: makeArchive(t, archiveEntry{name: binaryName, typeflag: tar.TypeSymlink, linkname: "other", size: 0})},
		{name: "hard link", archive: makeArchive(t, archiveEntry{name: binaryName, typeflag: tar.TypeLink, linkname: "other", size: 0})},
		{name: "duplicate candidate", archive: makeArchive(t, regularEntry(binaryName, validData), regularEntry(binaryName, validData))},
		{name: "truncated", archive: validArchive[:len(validArchive)-10]},
		{name: "oversized binary header", archive: makeOversizedArchive(t)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, target := newTarget(t, "old", 0755)
			state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", tc.archive)
			server := newTestReleaseServer(t, state)
			defer server.Close()
			config := updateConfig(server, target, "1.0.0", "darwin", "amd64")
			_, err := applyWith(context.Background(), config, updateSettings(server, target, "darwin", "amd64"))
			if err == nil {
				t.Fatal("expected malicious archive error")
			}
			if got := targetContent(t, target); got != "old" {
				t.Fatalf("target changed to %q after archive error", got)
			}
		})
	}
}

func TestApplyValidationFailureRollsBack(t *testing.T) {
	directory, target := newTarget(t, "old", 0755)
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
	server := newTestReleaseServer(t, state)
	defer server.Close()
	config := updateConfig(server, target, "1.0.0", "darwin", "amd64")
	config.Validate = func(ctx context.Context, path, expected string) error {
		if targetContent(t, path) != "new" {
			t.Error("validator did not see replacement")
		}
		return errors.New("validation failed")
	}
	_, err := applyWith(context.Background(), config, updateSettings(server, target, "darwin", "amd64"))
	if err == nil || !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("expected validation error, got %v", err)
	}
	if got := targetContent(t, target); got != "old" {
		t.Fatalf("target after rollback = %q, want old", got)
	}
	for _, name := range []string{".static-mangal-update.lock", ".static-mangal-update-backup"} {
		if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("transaction artifact %s remains: %v", name, err)
		}
	}
}

func TestApplyRejectsStaleAndConcurrentLocks(t *testing.T) {
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
	server := newTestReleaseServer(t, state)
	defer server.Close()

	staleDir, staleTarget := newTarget(t, "old", 0755)
	lockPath := filepath.Join(staleDir, ".static-mangal-update.lock")
	if err := os.WriteFile(lockPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := applyWith(context.Background(), updateConfig(server, staleTarget, "1.0.0", "darwin", "amd64"), updateSettings(server, staleTarget, "darwin", "amd64"))
	if !errors.Is(err, ErrLockExists) {
		t.Fatalf("expected stale lock error, got %v", err)
	}
	if targetContent(t, staleTarget) != "old" {
		t.Fatal("stale lock attempt changed target")
	}

	_, target := newTarget(t, "old", 0755)
	started := make(chan struct{})
	release := make(chan struct{})
	config := updateConfig(server, target, "1.0.0", "darwin", "amd64")
	var once sync.Once
	config.Validate = func(ctx context.Context, path, expected string) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	}
	firstDone := make(chan error, 1)
	go func() {
		_, firstErr := applyWith(context.Background(), config, updateSettings(server, target, "darwin", "amd64"))
		firstDone <- firstErr
	}()
	<-started
	_, err = applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), updateSettings(server, target, "darwin", "amd64"))
	if !errors.Is(err, ErrLockExists) {
		t.Fatalf("expected concurrent lock error, got %v", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first update failed: %v", err)
	}
}

func TestApplyRejectsHTTPRedirectAndUnsupportedPlatform(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("should not be downloaded"))
	}))
	defer plain.Close()
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
	state.archiveRedirect = plain.URL + "/archive"
	server := newTestReleaseServer(t, state)
	defer server.Close()
	_, target := newTarget(t, "old", 0755)
	_, err := applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), updateSettings(server, target, "darwin", "amd64"))
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected HTTPS redirect error, got %v", err)
	}
	if targetContent(t, target) != "old" {
		t.Fatal("redirect attempt changed target")
	}

	_, err = applyWith(context.Background(), Config{
		CurrentVersion: "1.0.0",
		Distribution:   "archive",
	}, testSettings{goos: "windows", goarch: "amd64"})
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("expected unsupported platform error, got %v", err)
	}
}

func TestApplyRejectsUnsafeTarget(t *testing.T) {
	archive := makeArchive(t, regularEntry(binaryName, []byte("new")))
	state := releaseStateForArchive(t, "v1.2.3", "Darwin", "amd64", archive)
	server := newTestReleaseServer(t, state)
	defer server.Close()

	directory := t.TempDir()
	realTarget := filepath.Join(directory, "real-target")
	if err := os.WriteFile(realTarget, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	symlinkTarget := filepath.Join(directory, binaryName)
	if err := os.Symlink(realTarget, symlinkTarget); err != nil {
		t.Fatal(err)
	}
	_, err := applyWith(context.Background(), updateConfig(server, symlinkTarget, "1.0.0", "darwin", "amd64"), updateSettings(server, symlinkTarget, "darwin", "amd64"))
	if err == nil || targetContent(t, realTarget) != "old" {
		t.Fatalf("expected symlink target refusal, got %v", err)
	}

	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, binaryName)
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0777); err != nil {
		t.Fatal(err)
	}
	_, err = applyWith(context.Background(), updateConfig(server, target, "1.0.0", "darwin", "amd64"), updateSettings(server, target, "darwin", "amd64"))
	if err == nil || targetContent(t, target) != "old" {
		t.Fatalf("expected writable parent refusal, got %v", err)
	}
}

func TestParseChecksumIsStrict(t *testing.T) {
	archiveFile := archiveName("v1.2.3", "Darwin", "amd64")
	sum := sha256.Sum256([]byte("archive"))
	valid := []byte(fmt.Sprintf("%x  %s\n", sum, archiveFile))
	if _, err := parseChecksum(valid, archiveFile); err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{
		[]byte(fmt.Sprintf("%x  %s\n%x  %s\n", sum, archiveFile, sum, archiveFile)),
		[]byte("not-a-checksum\n"),
		[]byte(fmt.Sprintf("%x  other.tar.gz\n", sum)),
	} {
		if _, err := parseChecksum(body, archiveFile); err == nil {
			t.Fatalf("accepted malformed checksum %q", body)
		}
	}
}

func TestDefaultAPIURLIsHTTPS(t *testing.T) {
	u, err := secureURL(DefaultAPIURL)
	if err != nil || u.Scheme != "https" {
		t.Fatalf("default API URL is not secure: %v", err)
	}
	if _, err := latestAPIURL("http://example.invalid"); err == nil {
		t.Fatal("accepted insecure API URL")
	}
}

func TestDefaultValidateUnixFixtures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable validation fixtures")
	}
	cases := []struct {
		name    string
		script  string
		wantErr bool
	}{
		{name: "exact version", script: "#!/bin/sh\nprintf '%s\\n' '1.2.3'"},
		{name: "version mismatch", script: "#!/bin/sh\nprintf '%s\\n' '1.2.4'", wantErr: true},
		{name: "oversized output", script: "#!/bin/sh\nprintf '%s' '" + strings.Repeat("x", validationOutput+1) + "'", wantErr: true},
		{name: "nonzero exit", script: "#!/bin/sh\nexit 7", wantErr: true},
		{name: "timeout", script: "#!/bin/sh\nexec sleep 30", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			target := filepath.Join(directory, binaryName)
			if err := os.WriteFile(target, []byte(tc.script+"\n"), 0755); err != nil {
				t.Fatal(err)
			}
			err := defaultValidate(context.Background(), target, "1.2.3")
			if tc.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestResolveExecutablePathsRejectsSymlinkedAncestors(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realDir := filepath.Join(base, "real")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, binaryName), []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(base, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}
	realExecutable := filepath.Join(realDir, binaryName)
	launchPath := filepath.Join(linkDir, binaryName)
	if _, _, err := resolveExecutablePaths(realExecutable, launchPath); err == nil || !strings.Contains(err.Error(), "launch path has a symlinked component") {
		t.Fatalf("expected launch-path ancestor refusal, got %v", err)
	}
}

func TestProductionDownloadURLPolicy(t *testing.T) {
	tag := "v1.2.3"
	filename := archiveName(tag, "Darwin", "amd64")
	valid := "https://github.com" + releaseDownloadPath(tag, filename)
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "valid", raw: valid, want: true},
		{name: "wrong repository", raw: strings.Replace(valid, "/ryanmccool/static-mangal/", "/other/repository/", 1)},
		{name: "wrong tag", raw: strings.Replace(valid, "/v1.2.3/", "/v1.2.4/", 1)},
		{name: "wrong filename", raw: strings.TrimSuffix(valid, filename) + "other.tar.gz"},
		{name: "query", raw: valid + "?download=1"},
		{name: "non-default port", raw: strings.Replace(valid, "https://github.com", "https://github.com:443", 1)},
		{name: "encoded path", raw: strings.Replace(valid, "static-mangal_", "static%2Dmangal_", 1)},
		{name: "traversal path", raw: strings.Replace(valid, "/releases/download/", "/releases/download/../download/", 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDownloadURL(tc.raw, tag, filename, "")
			if (err == nil) != tc.want {
				t.Fatalf("validateDownloadURL(%q) error = %v, want valid=%v", tc.raw, err, tc.want)
			}
		})
	}
}

func TestProductionRedirectPolicy(t *testing.T) {
	cases := []struct {
		name    string
		initial string
		target  string
		want    bool
	}{
		{name: "github to release CDN", initial: "github.com", target: "objects.githubusercontent.com", want: true},
		{name: "github remains", initial: "github.com", target: "github.com", want: true},
		{name: "release CDN remains", initial: "release-assets.githubusercontent.com", target: "objects.githubusercontent.com", want: true},
		{name: "wrong hostname boundary", initial: "github.com", target: "objects.githubusercontent.com.evil.example"},
		{name: "off origin", initial: "github.com", target: "evil.example"},
		{name: "API cannot reach CDN", initial: "api.github.com", target: "objects.githubusercontent.com"},
		{name: "API remains", initial: "api.github.com", target: "api.github.com", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowedRedirectHost(tc.initial, tc.target, ""); got != tc.want {
				t.Fatalf("allowedRedirectHost(%q, %q) = %v, want %v", tc.initial, tc.target, got, tc.want)
			}
		})
	}
}
