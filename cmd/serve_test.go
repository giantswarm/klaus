package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSOULFile_Exists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")
	content := "You are Klaus, a helpful AI assistant."

	if err := os.WriteFile(path, []byte(content), 0o444); err != nil {
		t.Fatalf("failed to write temp SOUL.md: %v", err)
	}

	got, err := loadSOULFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("expected %q, got %q", content, got)
	}
}

func TestLoadSOULFile_NotExists(t *testing.T) {
	got, err := loadSOULFile("/nonexistent/path/SOUL.md")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string for missing file, got %q", got)
	}
}

func TestLoadSOULFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")

	if err := os.WriteFile(path, []byte(""), 0o444); err != nil {
		t.Fatalf("failed to write temp SOUL.md: %v", err)
	}

	got, err := loadSOULFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string for empty file, got %q", got)
	}
}

func TestLoadSOULFile_WhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")

	if err := os.WriteFile(path, []byte("  \n\n  \t  \n"), 0o444); err != nil {
		t.Fatalf("failed to write temp SOUL.md: %v", err)
	}

	got, err := loadSOULFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string for whitespace-only file, got %q", got)
	}
}

func TestLoadSOULFile_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")
	content := "\n\n  You are a coding assistant.\n\n"

	if err := os.WriteFile(path, []byte(content), 0o444); err != nil {
		t.Fatalf("failed to write temp SOUL.md: %v", err)
	}

	got, err := loadSOULFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "You are a coding assistant."
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestLoadSOULFile_Unreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")

	if err := os.WriteFile(path, []byte("secret"), 0o000); err != nil {
		t.Fatalf("failed to write temp SOUL.md: %v", err)
	}

	// Skip if running as root (permissions not enforced).
	if os.Getuid() == 0 {
		t.Skip("skipping permission test as root")
	}

	_, err := loadSOULFile(path)
	if err == nil {
		t.Fatal("expected error for unreadable file, got nil")
	}
}

func TestLoadSOULFile_MultilineContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")
	content := "# Klaus Personality\n\nYou are Klaus.\nYou help with code.\n\n## Traits\n- Helpful\n- Concise"

	if err := os.WriteFile(path, []byte(content), 0o444); err != nil {
		t.Fatalf("failed to write temp SOUL.md: %v", err)
	}

	got, err := loadSOULFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("expected %q, got %q", content, got)
	}
}

func TestLoadSOULFile_ExceedsMaxSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")

	// Create a file that is one byte over the limit.
	data := strings.Repeat("A", maxSOULFileSize+1)
	if err := os.WriteFile(path, []byte(data), 0o444); err != nil {
		t.Fatalf("failed to write oversized SOUL.md: %v", err)
	}

	_, err := loadSOULFile(path)
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("expected size-related error, got: %v", err)
	}
}

func TestLoadSOULFile_ExactlyMaxSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")

	// Create a file at exactly the limit -- should succeed.
	data := strings.Repeat("B", maxSOULFileSize)
	if err := os.WriteFile(path, []byte(data), 0o444); err != nil {
		t.Fatalf("failed to write SOUL.md: %v", err)
	}

	got, err := loadSOULFile(path)
	if err != nil {
		t.Fatalf("unexpected error for max-size file: %v", err)
	}
	if got != data {
		t.Errorf("expected %d bytes of content, got %d", len(data), len(got))
	}
}

func TestSoulFilePath_Unset(t *testing.T) {
	// Ensure the env var is saved/restored, then unset it.
	t.Setenv("KLAUS_SOUL_FILE", "")
	os.Unsetenv("KLAUS_SOUL_FILE")

	got := soulFilePath()
	if got != defaultSOULPath {
		t.Errorf("expected default path %q, got %q", defaultSOULPath, got)
	}
}

func TestSoulFilePath_EmptyString(t *testing.T) {
	t.Setenv("KLAUS_SOUL_FILE", "")
	got := soulFilePath()
	if got != defaultSOULPath {
		t.Errorf("expected default path %q when env is empty string, got %q", defaultSOULPath, got)
	}
}

func TestSoulFilePath_EnvOverride(t *testing.T) {
	custom := "/var/lib/klaus/personality/SOUL.md"
	t.Setenv("KLAUS_SOUL_FILE", custom)
	got := soulFilePath()
	if got != custom {
		t.Errorf("expected %q, got %q", custom, got)
	}
}

// TestParseBool has moved to pkg/config/config_test.go alongside the config loader.
