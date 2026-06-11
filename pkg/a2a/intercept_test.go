package a2a

import "testing"

func TestInterceptSlashCommand(t *testing.T) {
	tests := []struct {
		input   string
		wantCmd string
		wantHit bool
	}{
		{"/config", "/config", true},
		{"/clear", "/clear", true},
		{"/compact some text", "/compact", true},
		{"/help me", "/help", true},
		{"/cost", "/cost", true},
		{"/doctor", "/doctor", true},
		{"/login now", "/login", true},
		{"/logout", "/logout", true},
		{"/status", "/status", true},
		// Not a slash command.
		{"hello world", "", false},
		{"", "", false},
		// Leading/trailing whitespace.
		{"  /clear  ", "/clear", true},
		// Not in the blocklist.
		{"/remember", "", false},
		{"/plan", "", false},
	}

	for _, tt := range tests {
		cmd, hit := interceptSlashCommand(tt.input)
		if hit != tt.wantHit {
			t.Errorf("interceptSlashCommand(%q) hit=%v, want %v", tt.input, hit, tt.wantHit)
		}
		if cmd != tt.wantCmd {
			t.Errorf("interceptSlashCommand(%q) cmd=%q, want %q", tt.input, cmd, tt.wantCmd)
		}
	}
}
