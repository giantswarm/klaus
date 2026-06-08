package a2a

import (
	"context"
	"fmt"
	"log"
	"maps"
	"net/url"
	"strings"
	"sync"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
)

// Registry is the outbound A2A call dispatcher. It holds static name→URL
// targets, an optional dynamic allowlist, and a per-host client cache.
// A nil *Registry disables the a2a_call MCP tool.
type Registry struct {
	targets      map[string]string  // name → base URL
	allowDynamic bool
	allowedHosts map[string]struct{} // host[:port] → present; empty = deny all dynamic

	mu      sync.Mutex
	clients map[string]*a2aclient.Client // host[:port] → cached client
}

// RegistryConfig is the static configuration used to construct a Registry.
type RegistryConfig struct {
	// Targets maps logical names to A2A base URLs.
	Targets map[string]string
	// AllowDynamic enables URL-based calls to hosts not covered by Targets.
	AllowDynamic bool
	// AllowedHosts lists the host[:port] values permitted for dynamic calls.
	// Must be non-empty when AllowDynamic is true.
	AllowedHosts []string
}

// NewRegistry constructs a Registry from cfg. Returns nil when cfg describes
// no outbound capability (no targets and dynamic disabled), which the caller
// may treat as "a2a_call not available".
func NewRegistry(cfg RegistryConfig) *Registry {
	if len(cfg.Targets) == 0 && !cfg.AllowDynamic {
		return nil
	}
	hosts := make(map[string]struct{}, len(cfg.AllowedHosts))
	for _, h := range cfg.AllowedHosts {
		hosts[h] = struct{}{}
	}
	return &Registry{
		targets:      cfg.Targets,
		allowDynamic: cfg.AllowDynamic,
		allowedHosts: hosts,
		clients:      make(map[string]*a2aclient.Client),
	}
}

// Call sends a message to the named or URL-addressed A2A agent and returns
// the concatenated text from the response. It blocks until the remote agent
// returns a terminal result.
func (r *Registry) Call(ctx context.Context, target, message string) (string, error) {
	baseURL, err := r.resolveURL(target)
	if err != nil {
		return "", err
	}

	client, err := r.clientFor(ctx, baseURL)
	if err != nil {
		return "", err
	}

	params := &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: message}),
	}
	result, err := client.SendMessage(ctx, params)
	if err != nil {
		return "", fmt.Errorf("A2A SendMessage to %q: %w", target, err)
	}

	task, ok := result.(*a2a.Task)
	if !ok || task.Status.State.Terminal() {
		return textFromResult(result), nil
	}

	return r.pollTask(ctx, client, task, target)
}

// pollTask streams task events via ResubscribeToTask until a terminal state is reached.
func (r *Registry) pollTask(ctx context.Context, client *a2aclient.Client, task *a2a.Task, target string) (string, error) {
	log.Printf("[a2a] task %q submitted to %q, polling until terminal", task.ID, target)

	for event, err := range client.ResubscribeToTask(ctx, &a2a.TaskIDParams{ID: task.ID}) {
		if err != nil {
			return "", fmt.Errorf("A2A ResubscribeToTask %q: %w", target, err)
		}
		switch e := event.(type) {
		case *a2a.TaskStatusUpdateEvent:
			log.Printf("[a2a] task %q state=%s final=%v", task.ID, e.Status.State, e.Final)
			if e.Status.State.Terminal() {
				if e.Status.Message != nil {
					if text := extractText(e.Status.Message); text != "" {
						return text, nil
					}
				}
				return "", nil
			}
		case *a2a.TaskArtifactUpdateEvent:
			// Artifact events carry partial output; skip — wait for terminal status.
		}
	}
	return "", fmt.Errorf("A2A task %q stream ended without terminal state", task.ID)
}

// Targets returns the configured static name→URL map (read-only copy).
func (r *Registry) Targets() map[string]string {
	return maps.Clone(r.targets)
}

// resolveURL returns the base URL for target (name or URL) or an error when
// the call must be denied.
func (r *Registry) resolveURL(target string) (string, error) {
	if rawURL, ok := r.targets[target]; ok {
		return rawURL, nil
	}
	if !r.allowDynamic {
		names := make([]string, 0, len(r.targets))
		for name := range r.targets {
			names = append(names, name)
		}
		if len(names) == 0 {
			return "", fmt.Errorf("dynamic A2A calls are disabled and no static targets are configured")
		}
		return "", fmt.Errorf("unknown A2A target %q (dynamic calls disabled); known targets: %s",
			target, strings.Join(names, ", "))
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("invalid A2A target URL %q: must be an absolute URL with a host", target)
	}
	host := parsed.Host
	// Fail-closed: deny all dynamic hosts when allowedHosts is empty.
	// Config.Validate() rejects allowDynamic=true + empty allowedHosts at startup;
	// this guard handles the case where the registry was not validate()d.
	if _, ok := r.allowedHosts[host]; !ok {
		return "", fmt.Errorf("A2A target host %q is not in the allowed-hosts list", host)
	}
	return target, nil
}

// clientFor returns a cached a2aclient.Client for baseURL, creating one via
// agent-card resolution on first access. The cache key is the parsed host.
func (r *Registry) clientFor(ctx context.Context, baseURL string) (*a2aclient.Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing A2A URL %q: %w", baseURL, err)
	}
	key := parsed.Host

	r.mu.Lock()
	defer r.mu.Unlock()

	if client, ok := r.clients[key]; ok {
		return client, nil
	}

	card, err := agentcard.DefaultResolver.Resolve(ctx, baseURL)
	if err != nil {
		return nil, fmt.Errorf("resolving agent card at %q: %w", baseURL, err)
	}

	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		return nil, fmt.Errorf("creating A2A client for %q: %w", baseURL, err)
	}

	r.clients[key] = client
	return client, nil
}

// textFromResult extracts concatenated text from a SendMessageResult.
// Handles both *a2a.Message (direct response) and *a2a.Task (deferred / polled).
func textFromResult(result a2a.SendMessageResult) string {
	switch v := result.(type) {
	case *a2a.Message:
		return extractText(v)
	case *a2a.Task:
		if v.Status.Message != nil {
			if text := extractText(v.Status.Message); text != "" {
				return text
			}
		}
		// Fall back to the most recent agent message in history.
		for i := len(v.History) - 1; i >= 0; i-- {
			if v.History[i].Role == a2a.MessageRoleAgent {
				if text := extractText(v.History[i]); text != "" {
					return text
				}
			}
		}
	}
	return ""
}
