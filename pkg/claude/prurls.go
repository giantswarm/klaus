package claude

import (
	"encoding/json"
	"regexp"
	"strings"
)

// PR URL attribution.
//
// pr_urls lists the pull requests a turn created or pushed to. It is derived
// from the Bash tool calls of the turn, never from arbitrary URLs that happen
// to appear in tool output:
//
//   - `gh pr create`: every PR URL the command prints is a PR the turn created.
//   - `git push`: the GitHub repository named in the "To <remote>" line of the
//     push output is a repository the turn pushed to.
//   - `gh pr <subcommand>` addressing a single PR (view, checkout, merge,
//     comment, edit, review, checks, ...): the PR named by URL (in the command
//     or its output) or by number (resolved against --repo, or against the
//     single repository the turn pushed to) counts when its repository is one
//     the turn pushed to.
//
// PR URLs merely read during research (`gh pr view` in a repository the turn
// never pushed to, file contents, `gh pr list` output) are excluded.

// prURLPattern matches GitHub pull request URLs. The first submatch is the
// owner/repo path.
var prURLPattern = regexp.MustCompile(`https://github\.com/([\w.\-]+/[\w.\-]+)/pull/\d+`)

// pushTargetPattern matches the "To <remote>" summary line that git push
// prints, for GitHub remotes in https, ssh and scp-like syntax. The first
// submatch is the owner/repo path.
var pushTargetPattern = regexp.MustCompile(
	`(?m)^To\s+(?:https?://(?:[^@\s/]+@)?github\.com/|(?:ssh://)?git@github\.com[:/]|github\.com[:/])([\w.\-]+/[\w.\-]+?)(?:\.git)?/?\s*$`)

// gitPushPattern matches a git push invocation inside one shell segment.
var gitPushPattern = regexp.MustCompile(`(?:^|\s)git\b.*\bpush\b`)

// ghPRPattern matches a `gh pr <subcommand> [args...]` invocation inside one
// shell segment. Submatch 1 is the subcommand, submatch 2 the raw arguments.
var ghPRPattern = regexp.MustCompile(`(?:^|\s)gh\s+pr\s+([a-z][a-z-]*)((?:\s+\S+)*)`)

// shellSeparatorPattern splits a command into segments at pipes, command
// separators and newlines. Quoting is not honoured; the heuristic only needs
// to isolate the git/gh invocation from the commands around it.
var shellSeparatorPattern = regexp.MustCompile(`\s*(?:&&|\|\||;|\||\n)\s*`)

// ghPRSingleTargetSubcommands are the gh pr subcommands that act on one PR.
var ghPRSingleTargetSubcommands = map[string]bool{
	"checkout":      true,
	"checks":        true,
	"close":         true,
	"comment":       true,
	"diff":          true,
	"edit":          true,
	"lock":          true,
	"merge":         true,
	"ready":         true,
	"reopen":        true,
	"review":        true,
	"unlock":        true,
	"update-branch": true,
	"view":          true,
}

// ghPRSubcommandCreate opens a new pull request.
const ghPRSubcommandCreate = "create"

// maxPRURLsPerBlock caps the number of PR URLs extracted from a single content block.
const maxPRURLsPerBlock = 20

// prRef is a candidate pull request reference found in a turn. Either url is
// set, or number (with an optional owner/repo qualifier) is.
type prRef struct {
	url     string
	number  string
	repo    string
	created bool
}

// ghPRInvocation is one parsed `gh pr <subcommand>` call.
type ghPRInvocation struct {
	sub    string
	url    string // PR given as URL argument
	number string // PR given as number argument
	repo   string // --repo / -R value, normalised to owner/repo
}

// bashCommand returns the command of a Bash tool_use message, or "" when the
// message is not a Bash invocation.
func bashCommand(msg StreamMessage) string {
	if msg.Type != MessageTypeAssistant || msg.Subtype != SubtypeToolUse || !isBashTool(msg.ToolName) || len(msg.ToolArgs) == 0 {
		return ""
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(msg.ToolArgs, &args); err != nil {
		return ""
	}
	return args.Command
}

// isBashTool reports whether the given tool name refers to the Bash/shell tool.
func isBashTool(name string) bool {
	return name == ToolNameBash || name == "bash"
}

// extractPRURLs returns the GitHub PR URLs found in text, capped at maxPRURLsPerBlock.
func extractPRURLs(text string) []string {
	return prURLPattern.FindAllString(text, maxPRURLsPerBlock)
}

// repoOfPRURL returns the lower-cased owner/repo path of a PR URL.
func repoOfPRURL(url string) string {
	m := prURLPattern.FindStringSubmatch(url)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(m[1])
}

// normalizeRepo turns a --repo argument (owner/repo, github.com/owner/repo or
// a full URL) into a lower-cased owner/repo path.
func normalizeRepo(s string) string {
	s = strings.Trim(s, `"'`)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "github.com/")
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	if strings.Count(s, "/") != 1 {
		return ""
	}
	return strings.ToLower(s)
}

// isDigits reports whether s is a non-empty string of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseGHPRInvocations returns the gh pr invocations in one shell segment.
func parseGHPRInvocations(segment string) []ghPRInvocation {
	var invocations []ghPRInvocation
	for _, m := range ghPRPattern.FindAllStringSubmatch(segment, -1) {
		inv := ghPRInvocation{sub: m[1]}
		tokens := strings.Fields(m[2])
		for i := 0; i < len(tokens); i++ {
			tok := tokens[i]
			switch {
			case tok == "-R" || tok == "--repo":
				if i+1 < len(tokens) {
					inv.repo = normalizeRepo(tokens[i+1])
					i++
				}
			case strings.HasPrefix(tok, "--repo="):
				inv.repo = normalizeRepo(strings.TrimPrefix(tok, "--repo="))
			case strings.HasPrefix(tok, "-"):
				// Other flags; their values are skipped by the positional
				// checks below (they are neither URLs nor numbers).
			case inv.url == "" && inv.number == "":
				tok = strings.Trim(tok, `"'`)
				if u := prURLPattern.FindString(tok); u != "" {
					inv.url = u
				} else if isDigits(tok) {
					inv.number = tok
				}
			}
		}
		invocations = append(invocations, inv)
	}
	return invocations
}

// CollectPRURLs returns the unique pull request URLs a turn created or pushed
// to, in order of first appearance. See the package comment at the top of this
// file for the attribution rules.
func CollectPRURLs(messages []StreamMessage) []string {
	// Map Bash tool_use IDs to their commands.
	commands := make(map[string]string)
	for _, msg := range messages {
		if cmd := bashCommand(msg); cmd != "" && msg.ToolID != "" {
			commands[msg.ToolID] = cmd
		}
	}
	if len(commands) == 0 {
		return nil
	}

	pushed := make(map[string]bool)
	var refs []prRef
	for _, msg := range messages {
		for _, block := range ExtractToolResults(msg) {
			cmd, ok := commands[block.ToolUseID]
			if !ok {
				continue
			}
			for _, segment := range shellSeparatorPattern.Split(cmd, -1) {
				if gitPushPattern.MatchString(segment) {
					for _, m := range pushTargetPattern.FindAllStringSubmatch(block.Content, -1) {
						pushed[strings.ToLower(m[1])] = true
					}
				}
				for _, inv := range parseGHPRInvocations(segment) {
					switch {
					case inv.sub == ghPRSubcommandCreate:
						for _, u := range extractPRURLs(block.Content) {
							refs = append(refs, prRef{url: u, created: true})
							pushed[repoOfPRURL(u)] = true
						}
					case ghPRSingleTargetSubcommands[inv.sub]:
						switch {
						case inv.url != "":
							refs = append(refs, prRef{url: inv.url})
						case inv.number != "":
							refs = append(refs, prRef{number: inv.number, repo: inv.repo})
						default:
							// Addressed by branch name or the current branch:
							// gh prints the PR URL in its output.
							for _, u := range extractPRURLs(block.Content) {
								refs = append(refs, prRef{url: u})
							}
						}
					}
				}
			}
		}
	}

	// A bare PR number is resolved against the only repository the turn
	// pushed to; with several candidates the reference stays ambiguous.
	var singleRepo string
	if len(pushed) == 1 {
		for repo := range pushed {
			singleRepo = repo
		}
	}

	var urls []string
	for _, ref := range refs {
		url := ref.url
		if url == "" {
			repo := ref.repo
			if repo == "" {
				repo = singleRepo
			}
			if repo == "" {
				continue
			}
			url = "https://github.com/" + repo + "/pull/" + ref.number
		}
		if ref.created || pushed[repoOfPRURL(url)] {
			urls = appendUnique(urls, url)
		}
	}
	if len(urls) == 0 {
		return nil
	}
	return urls
}
