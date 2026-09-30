package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeClaude puts a claude script with the given body first on PATH.
func fakeClaude(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o700); err != nil { // #nosec G306 -- test helper must be executable
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestProcess_ExitError(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus ProcessStatus
		wantError  string
		wantResult string
	}{
		{
			name:       "the agent's message names the cause",
			body:       `echo '{"type":"result","subtype":"success","is_error":true,"result":"Not logged in · Please run /login"}'; exit 1`,
			wantStatus: ProcessStatusError,
			wantError:  "exit status 1: Not logged in · Please run /login",
			wantResult: "Not logged in · Please run /login",
		},
		{
			name:       "without a message the stderr tail names it",
			body:       `echo 'first line' >&2; echo 'fatal: no credential' >&2; exit 2`,
			wantStatus: ProcessStatusError,
			wantError:  "exit status 2: first line\nfatal: no credential",
		},
		{
			name:       "without a message or stderr the exit status stays",
			body:       `exit 3`,
			wantStatus: ProcessStatusError,
			wantError:  "exit status 3",
		},
		{
			name:       "a successful turn is unchanged",
			body:       `echo 'noise' >&2; echo '{"type":"result","subtype":"success","result":"done"}'`,
			wantStatus: ProcessStatusIdle,
			wantResult: "done",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClaude(t, tt.body)
			opts := DefaultOptions()
			opts.ResultDir = t.TempDir()
			p := NewProcess(opts)

			if _, _, err := p.RunSync(context.Background(), "hello"); err != nil {
				t.Fatalf("RunSync: %v", err)
			}

			status := p.Status()
			if status.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", status.Status, tt.wantStatus)
			}
			if status.ErrorMessage != tt.wantError {
				t.Errorf("Status().ErrorMessage = %q, want %q", status.ErrorMessage, tt.wantError)
			}

			persisted, err := NewResultStore(opts.ResultDir).Load()
			if err != nil {
				t.Fatalf("load persisted result: %v", err)
			}
			if persisted.ErrorMessage != tt.wantError {
				t.Errorf("persisted ErrorMessage = %q, want %q", persisted.ErrorMessage, tt.wantError)
			}
			if persisted.ResultText != tt.wantResult {
				t.Errorf("persisted ResultText = %q, want %q", persisted.ResultText, tt.wantResult)
			}
		})
	}
}

func TestExitErrorTruncatesDetail(t *testing.T) {
	long := make([]rune, maxExitDetailLen+10)
	for i := range long {
		long[i] = 'x'
	}
	turn := []StreamMessage{{Type: MessageTypeResult, Result: string(long)}}
	got := exitError(errors.New("exit status 1"), turn, nil)
	want := "exit status 1: " + string(long[:maxExitDetailLen]) + "..."
	if got != want {
		t.Errorf("exitError length = %d, want %d", len(got), len(want))
	}
}
