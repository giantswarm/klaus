package a2a

import (
	"context"
	"fmt"
	"log"
	"maps"
	"net/url"
	"strings"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
)

// Registry is the outbound A2A call dispatcher. It holds static name→URL
// targets, an optional dynamic allowlist, and a per-host client cache.
// A nil *Registry disables the a2a_call MCP tool.
type Registry struct {
	targets      map[string]string // name → base URL
	allowDynamic bool
	allowedHosts map[string]struct{} // normalized host[:port] → present; empty = deny all dynamic

	mu      sync.RWMutex
	clients map[string]*a2aclient.Client // scheme://host[:port]/path → cached client
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

	req := &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(message)),
	}
	result, err := client.SendMessage(ctx, req)
	if err != nil {
		return "", fmt.Errorf("A2A SendMessage to %q: %w", target, err)
	}

	task, ok := result.(*a2a.Task)
	if !ok || task.Status.State.Terminal() {
		return textFromResult(result), nil
	}

	return r.pollTask(ctx, client, task, target)
}

// pollTask streams task events via SubscribeToTask until a terminal state is reached.
// Artifacts are accumulated per artifact ID, honouring the Append flag (false = snapshot,
// replacing prior content for that artifact ID). When the terminal status carries no
// message, the accumulated artifact text is returned.
func (r *Registry) pollTask(ctx context.Context, client *a2aclient.Client, task *a2a.Task, target string) (string, error) {
	log.Printf("[a2a] task %q submitted to %q, polling until terminal", task.ID, target)

	artifacts := make(map[a2a.ArtifactID]*strings.Builder)
	var artifactOrder []a2a.ArtifactID

	for event, err := range client.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{ID: task.ID}) {
		if err != nil {
			return "", fmt.Errorf("A2A SubscribeToTask %q: %w", target, err)
		}
		switch e := event.(type) {
		case *a2a.TaskStatusUpdateEvent:
			log.Printf("[a2a] task %q state=%s", task.ID, e.Status.State)
			if e.Status.State.Terminal() {
				if text := extractText(e.Status.Message); text != "" {
					return text, nil
				}
				var result strings.Builder
				for _, id := range artifactOrder {
					if b, ok := artifacts[id]; ok {
						result.WriteString(b.String())
					}
				}
				return result.String(), nil
			}
		case *a2a.TaskArtifactUpdateEvent:
			if e.Artifact != nil {
				id := e.Artifact.ID
				b, exists := artifacts[id]
				if !exists || !e.Append {
					// First chunk or snapshot (Append=false replaces previous content).
					b = &strings.Builder{}
					artifacts[id] = b
				}
				for _, part := range e.Artifact.Parts {
					if part != nil {
						b.WriteString(part.Text())
					}
				}
				if !exists {
					artifactOrder = append(artifactOrder, id)
				}
			}
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
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("A2A target URL scheme %q is not permitted; only http and https are allowed", parsed.Scheme)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("A2A target URL must not contain userinfo")
	}
	// Fail-closed: deny all dynamic hosts when allowedHosts is empty.
	// Config.Validate() rejects allowDynamic=true + empty allowedHosts at startup;
	// this guard handles the case where the registry was not validate()d.
	if _, ok := r.allowedHosts[normalizedHost(parsed)]; !ok {
		return "", fmt.Errorf("A2A target host %q is not in the allowed-hosts list", parsed.Host)
	}
	return target, nil
}

// clientFor returns a cached a2aclient.Client for baseURL, creating one via
// agent-card resolution on first access. The cache key includes scheme, host,
// and path so two agents at the same host+path over different schemes get
// independent clients. Network I/O happens outside the lock so concurrent calls
// to different hosts do not serialize behind a slow resolution.
func (r *Registry) clientFor(ctx context.Context, baseURL string) (*a2aclient.Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing A2A URL %q: %w", baseURL, err)
	}
	key := parsed.Scheme + "://" + parsed.Host + parsed.Path

	r.mu.RLock()
	client, ok := r.clients[key]
	r.mu.RUnlock()
	if ok {
		return client, nil
	}

	card, err := agentcard.DefaultResolver.Resolve(ctx, baseURL)
	if err != nil {
		return nil, fmt.Errorf("resolving agent card at %q: %w", baseURL, err)
	}

	// Validate the card's interface URLs against the allowlist. An allowlisted
	// host could otherwise return a card that redirects actual traffic to an
	// internal service.
	for _, iface := range card.SupportedInterfaces {
		if iface == nil {
			continue
		}
		if err := r.validateCardInterfaceURL(baseURL, iface.URL); err != nil {
			return nil, fmt.Errorf("agent card for %q rejected: %w", baseURL, err)
		}
	}

	client, err = a2aclient.NewFromCard(ctx, card)
	if err != nil {
		return nil, fmt.Errorf("creating A2A client for %q: %w", baseURL, err)
	}

	r.mu.Lock()
	if existing, exists := r.clients[key]; exists {
		r.mu.Unlock()
		return existing, nil
	}
	r.clients[key] = client
	r.mu.Unlock()
	return client, nil
}

// validateCardInterfaceURL checks that a card-advertised interface URL is safe
// to use. When allowedHosts is non-empty (dynamic mode), the interface host
// must be in the allowlist. When allowedHosts is empty (static-only mode), the
// interface host must match the original target host to prevent open-redirect
// attacks via a compromised static target's card.
func (r *Registry) validateCardInterfaceURL(targetBaseURL, ifaceURL string) error {
	iface, err := url.Parse(ifaceURL)
	if err != nil || iface.Host == "" {
		return fmt.Errorf("invalid interface URL %q", ifaceURL)
	}
	if iface.Scheme != "http" && iface.Scheme != "https" {
		return fmt.Errorf("interface URL scheme %q is not permitted", iface.Scheme)
	}
	ifaceHost := normalizedHost(iface)
	if len(r.allowedHosts) > 0 {
		if _, ok := r.allowedHosts[ifaceHost]; !ok {
			return fmt.Errorf("interface host %q is not in the allowed-hosts list", iface.Host)
		}
		return nil
	}
	// Static-only mode: the interface must stay on the same host as the target.
	target, _ := url.Parse(targetBaseURL)
	if ifaceHost != normalizedHost(target) {
		return fmt.Errorf("interface host %q does not match target host %q", iface.Host, target.Host)
	}
	return nil
}

// normalizedHost returns the host from a parsed URL with default ports stripped
// (port 80 for http, port 443 for https). This lets allowedHosts entries use
// bare hostnames regardless of whether the caller included the default port.
func normalizedHost(u *url.URL) string {
	h := u.Hostname()
	port := u.Port()
	if port == "" {
		return h
	}
	switch u.Scheme {
	case "http":
		if port == "80" {
			return h
		}
	case "https":
		if port == "443" {
			return h
		}
	}
	return h + ":" + port
}

// textFromResult extracts concatenated text from a SendMessageResult.
// Handles both *a2a.Message (direct response) and *a2a.Task (deferred / polled).
// Priority: status message → artifacts → history (newest agent message).
func textFromResult(result a2a.SendMessageResult) string {
	switch v := result.(type) {
	case *a2a.Message:
		return extractText(v)
	case *a2a.Task:
		if text := extractText(v.Status.Message); text != "" {
			return text
		}
		// Many agents (including Klaus) emit output as artifact events rather
		// than attaching text to the terminal status message.
		var artifactSB strings.Builder
		for _, artifact := range v.Artifacts {
			if artifact == nil {
				continue
			}
			for _, part := range artifact.Parts {
				if part != nil {
					artifactSB.WriteString(part.Text())
				}
			}
		}
		if s := artifactSB.String(); s != "" {
			return s
		}
		// Last resort: most recent agent message in history.
		for i := len(v.History) - 1; i >= 0; i-- {
			if v.History[i] != nil && v.History[i].Role == a2a.MessageRoleAgent {
				if text := extractText(v.History[i]); text != "" {
					return text
				}
			}
		}
	}
	return ""
}
