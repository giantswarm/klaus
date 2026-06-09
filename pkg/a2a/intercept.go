package a2a

import "strings"

// unsupportedCommands are Claude Code interactive TUI commands that require a
// terminal and don't work headless. Intercepted early so they never reach the
// subprocess.
var unsupportedCommands = []string{
	"/config", "/clear", "/compact", "/help", "/cost", //nolint:goconst
	"/doctor", "/login", "/logout", "/status", //nolint:goconst
}

// interceptSlashCommand returns the matched command and true when the text
// is exactly a TUI-only slash command or the command followed by a space
// (e.g. "/help me"). A bare prefix without a word boundary is not matched
// so "/costs" does not intercept "/cost".
func interceptSlashCommand(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	for _, cmd := range unsupportedCommands {
		if trimmed == cmd || strings.HasPrefix(trimmed, cmd+" ") {
			return cmd, true
		}
	}
	return "", false
}
