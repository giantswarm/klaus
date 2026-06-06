package a2a

import "strings"

// unsupportedCommands are Claude Code interactive TUI commands that require a
// terminal and don't work headless. Intercepted early so they never reach the
// subprocess.
var unsupportedCommands = []string{
	"/config", "/clear", "/compact", "/help", "/cost",
	"/doctor", "/login", "/logout", "/status",
}

// interceptSlashCommand returns the matched command and true when the text
// starts with a TUI-only slash command. The match is prefix-based so
// "/help me" also matches "/help".
func interceptSlashCommand(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	for _, cmd := range unsupportedCommands {
		if strings.HasPrefix(trimmed, cmd) {
			return cmd, true
		}
	}
	return "", false
}
