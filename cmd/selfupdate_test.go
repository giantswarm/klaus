package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

func TestNewSelfUpdateCmd(t *testing.T) {
	// Test self-update command creation
	selfUpdateCmd := newSelfUpdateCmd()

	if selfUpdateCmd.Use != "self-update" {
		t.Errorf("Expected Use to be 'self-update', got %s", selfUpdateCmd.Use)
	}

	if selfUpdateCmd.Short == "" {
		t.Error("Expected Short description to be set")
	}

	if selfUpdateCmd.Long == "" {
		t.Error("Expected Long description to be set")
	}

	if selfUpdateCmd.RunE == nil {
		t.Error("Expected RunE function to be set")
	}
}

func TestRunSelfUpdateWithDevVersion(t *testing.T) {
	// Test self-update with development version
	originalVersion := rootCmd.Version
	defer func() { rootCmd.Version = originalVersion }()

	// Test with "dev" version
	rootCmd.Version = "dev"

	err := runSelfUpdate(nil, []string{})
	if err == nil {
		t.Error("Expected error for dev version")
	}

	if !strings.Contains(err.Error(), "cannot self-update a development version") {
		t.Errorf("Expected specific error message, got: %s", err.Error())
	}
}

func TestRunSelfUpdateWithEmptyVersion(t *testing.T) {
	// Test self-update with empty version
	originalVersion := rootCmd.Version
	defer func() { rootCmd.Version = originalVersion }()

	rootCmd.Version = ""

	err := runSelfUpdate(nil, []string{})
	if err == nil {
		t.Error("Expected error for empty version")
	}

	if !strings.Contains(err.Error(), "cannot self-update a development version") {
		t.Errorf("Expected specific error message, got: %s", err.Error())
	}
}

func TestSelfUpdateCommandHelp(t *testing.T) {
	// Test self-update command help
	selfUpdateCmd := newSelfUpdateCmd()
	var buf bytes.Buffer
	selfUpdateCmd.SetOut(&buf)
	selfUpdateCmd.SetErr(&buf) // Also capture stderr for help
	selfUpdateCmd.SetArgs([]string{"--help"})

	err := selfUpdateCmd.Execute()
	if err != nil {
		t.Fatalf("Error executing self-update help: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Checks for the latest release") {
		t.Errorf("Help output should contain long description. Got: %q", output)
	}
	if !strings.Contains(output, "Sigstore bundle") {
		t.Errorf("Help output should say that releases are verified. Got: %q", output)
	}
}

// The signature check itself (a bundle that verifies, a tampered binary, a
// bundle for another repository) is tested where it lives, in
// github.com/giantswarm/selfupdate-cosign. What follows checks that klaus
// wires it in so that an unsigned or unverifiable release never reaches the
// disk.

// fakeSource stands in for GitHub: one release, and the bytes every asset
// download returns.
type fakeSource struct {
	release fakeRelease
	assets  map[int64][]byte
	// listCalls counts how often the command asked for releases, so a test can
	// assert that a refusal happened before GitHub was consulted at all.
	listCalls int
}

func (s *fakeSource) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	s.listCalls++
	return []selfupdate.SourceRelease{s.release}, nil
}

func (s *fakeSource) DownloadReleaseAsset(_ context.Context, _ *selfupdate.Release, id int64) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.assets[id])), nil
}

type fakeAsset struct {
	id   int64
	name string
}

func (a fakeAsset) GetID() int64                  { return a.id }
func (a fakeAsset) GetName() string               { return a.name }
func (a fakeAsset) GetSize() int                  { return 3 }
func (a fakeAsset) GetBrowserDownloadURL() string { return "https://example.test/" + a.name }

type fakeRelease struct {
	tag    string
	assets []selfupdate.SourceAsset
}

func (r fakeRelease) GetID() int64              { return 1 }
func (r fakeRelease) GetTagName() string        { return r.tag }
func (r fakeRelease) GetDraft() bool            { return false }
func (r fakeRelease) GetPrerelease() bool       { return false }
func (r fakeRelease) GetPublishedAt() time.Time { return time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC) }
func (r fakeRelease) GetReleaseNotes() string   { return "notes" }
func (r fakeRelease) GetName() string           { return r.tag }
func (r fakeRelease) GetURL() string {
	return "https://github.com/giantswarm/klaus/releases/tag/" + r.tag
}
func (r fakeRelease) GetAssets() []selfupdate.SourceAsset { return r.assets }

// binaryAsset is the asset name architect publishes for this platform.
func binaryAsset() string {
	name := "klaus-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// selfUpdateFixture points the command at src instead of GitHub and at a
// throwaway file instead of the running executable; it returns that file's
// path and its content, so a test can assert the file survived.
func selfUpdateFixture(t *testing.T, src *fakeSource) (string, []byte) {
	t.Helper()
	installed := []byte("the klaus that is installed right now")
	exe := filepath.Join(t.TempDir(), "klaus")
	if err := os.WriteFile(exe, installed, 0o755); err != nil { //nolint:gosec // an executable
		t.Fatal(err)
	}
	prevSource, prevExe, prevVersion := selfUpdateSource, selfUpdateExecutable, rootCmd.Version
	selfUpdateSource = src
	selfUpdateExecutable = func() (string, error) { return exe, nil }
	rootCmd.Version = "v1.0.0"
	t.Cleanup(func() {
		selfUpdateSource, selfUpdateExecutable, rootCmd.Version = prevSource, prevExe, prevVersion
	})
	return exe, installed
}

func assertUnchanged(t *testing.T, exe string, installed []byte) {
	t.Helper()
	got, err := os.ReadFile(exe) //nolint:gosec // the test's own temp file
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, installed) {
		t.Fatalf("the installed binary was replaced: %q", got)
	}
}

func TestRunSelfUpdateRefusesAReleaseWithoutASignatureBundle(t *testing.T) {
	src := &fakeSource{
		release: fakeRelease{tag: "v99.0.0", assets: []selfupdate.SourceAsset{fakeAsset{1, binaryAsset()}}},
		assets:  map[int64][]byte{1: []byte("a newer klaus, unsigned")},
	}
	exe, installed := selfUpdateFixture(t, src)

	err := runSelfUpdate(nil, nil)
	if err == nil {
		t.Fatal("a release without a bundle must be refused")
	}
	if !strings.Contains(err.Error(), "no signature bundle") {
		t.Errorf("the error should say what is missing, got: %v", err)
	}
	assertUnchanged(t, exe, installed)
}

func TestRunSelfUpdateRefusesADownloadThatDoesNotVerify(t *testing.T) {
	src := &fakeSource{
		release: fakeRelease{tag: "v99.0.0", assets: []selfupdate.SourceAsset{
			fakeAsset{1, binaryAsset()},
			fakeAsset{2, binaryAsset() + ".bundle"},
		}},
		assets: map[int64][]byte{
			1: []byte("a newer klaus"),
			2: []byte("{}"), // not a Sigstore bundle
		},
	}
	exe, installed := selfUpdateFixture(t, src)

	var out bytes.Buffer
	cmd := newSelfUpdateCmd()
	cmd.SetOut(&out)
	err := runSelfUpdate(cmd, nil)
	if err == nil {
		t.Fatal("a download whose bundle does not verify must be refused")
	}
	if !strings.Contains(err.Error(), "is unchanged") || !strings.Contains(err.Error(), "is not a Sigstore bundle") {
		t.Errorf("the error should say the binary was refused and why, got: %v", err)
	}
	if !strings.Contains(out.String(), "Found newer version: 99.0.0") {
		t.Errorf("the newer release should have been announced before the refusal, got:\n%s", out.String())
	}
	assertUnchanged(t, exe, installed)
}

func TestRunSelfUpdateRefusesANonReleaseVersion(t *testing.T) {
	// A binary built from main (not from a tag) reports its git SHA as the
	// version. go-selfupdate's Release.GreaterThan would panic parsing that as
	// semver; the command must refuse it before it ever asks for releases.
	// The release is complete (binary and bundle), so nothing else would stop
	// the command before that comparison.
	src := &fakeSource{
		release: fakeRelease{tag: "v99.0.0", assets: []selfupdate.SourceAsset{
			fakeAsset{1, binaryAsset()},
			fakeAsset{2, binaryAsset() + ".bundle"},
		}},
		assets: map[int64][]byte{1: []byte("a newer klaus"), 2: []byte("{}")},
	}
	exe, installed := selfUpdateFixture(t, src)
	const sha = "39d5c3f5165c0d2b8a4e7f6c1b3d9e0a2f4c6e8b"
	rootCmd.Version = sha

	err := runSelfUpdate(nil, nil)
	if err == nil {
		t.Fatal("a version that is not semver must be refused")
	}
	want := "cannot self-update: the running binary reports version \"" + sha + "\", which is not a release version (was it built from a tag?)"
	if err.Error() != want {
		t.Errorf("error should name the non-release version,\n got: %v\nwant: %s", err, want)
	}
	if src.listCalls != 0 {
		t.Errorf("the source must not be asked for releases, ListReleases was called %d time(s)", src.listCalls)
	}
	assertUnchanged(t, exe, installed)
}
