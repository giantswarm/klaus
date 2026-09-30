package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPromptWhileBusy checks that a prompt arriving while another one runs
// is refused with ErrStarting until the session's init message, then with
// plain ErrBusy, for both process kinds.
func TestPromptWhileBusy(t *testing.T) {
	gates := t.TempDir()
	started := filepath.Join(gates, "started")
	finished := filepath.Join(gates, "finished")
	waitFor := func(file string) string {
		return `while [ ! -f "` + file + `" ]; do sleep 0.02; done; `
	}
	init := `echo '{"type":"system","subtype":"init","session_id":"s1"}'; `
	result := `echo '{"type":"result","subtype":"success","result":"done"}'`

	tests := []struct {
		name   string
		body   string
		newRun func(opts Options) Prompter
	}{
		{
			name: "process",
			body: waitFor(started) + init + waitFor(finished) + result,
			newRun: func(opts Options) Prompter {
				return NewProcess(opts)
			},
		},
		{
			name: "persistent process",
			body: `read -r line; ` + waitFor(started) + init + waitFor(finished) + result + `; while read -r line; do :; done`,
			newRun: func(opts Options) Prompter {
				return NewPersistentProcess(opts)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, f := range []string{started, finished} {
				_ = os.Remove(f)
			}
			fakeClaude(t, tt.body)
			opts := DefaultOptions()
			opts.ResultDir = t.TempDir()
			p := tt.newRun(opts)
			t.Cleanup(func() { _ = p.Stop() })

			ch, err := p.Run(context.Background(), "first")
			if err != nil {
				t.Fatalf("first Run: %v", err)
			}

			_, err = p.Run(context.Background(), "during start")
			if !errors.Is(err, ErrStarting) || !errors.Is(err, ErrBusy) {
				t.Fatalf("Run while starting: err = %v, want ErrStarting wrapping ErrBusy", err)
			}

			touch(t, started)
			waitForMessage(t, ch, func(m StreamMessage) bool { return isInit(m) })

			_, err = p.Run(context.Background(), "during the turn")
			if !errors.Is(err, ErrBusy) || errors.Is(err, ErrStarting) {
				t.Fatalf("Run during the turn: err = %v, want ErrBusy only", err)
			}

			touch(t, finished)
			waitForMessage(t, ch, func(m StreamMessage) bool { return m.Type == MessageTypeResult })
			drain(t, ch)
			// The turn's end is recorded after its stream closes.
			if err := waitIdle(p); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func touch(t *testing.T, file string) {
	t.Helper()
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("touch %s: %v", file, err)
	}
}

// waitForMessage reads ch until a message matches or a deadline passes.
func waitForMessage(t *testing.T, ch <-chan StreamMessage, match func(StreamMessage) bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case m, ok := <-ch:
			if !ok {
				t.Fatal("stream closed before the expected message")
			}
			if match(m) {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for a message")
		}
	}
}

// drain reads ch until it is closed.
func drain(t *testing.T, ch <-chan StreamMessage) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for the stream to close")
		}
	}
}

// waitIdle waits until p takes a new prompt, i.e. its turn has ended.
func waitIdle(p Prompter) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := p.Status().Status; s != ProcessStatusBusy && s != ProcessStatusStarting {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("timed out waiting for the turn to end")
}
