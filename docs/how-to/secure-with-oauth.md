# Secure with OAuth

Protect the `/mcp` endpoint with OAuth 2.1 authentication. Klaus supports Dex and Google as OIDC providers via the `mcp-oauth` library.

## Enable OAuth

OAuth is configured via environment variables. The Helm chart wires these from values:

```yaml
# These are passed as environment variables to the klaus container.
# Consult the mcp-oauth library documentation for the full set of options.
```

When OAuth is enabled, the `/mcp` endpoint requires a valid bearer token. Operational endpoints (`/healthz`, `/readyz`, `/status`, `/metrics`) remain unauthenticated.

## Owner-based access control

Restrict instance access to a specific user by setting the owner subject:

```yaml
owner:
  subject: "user@example.com"  # sub or email claim from the ID token
```

This sets `KLAUS_OWNER_SUBJECT` on the container. When set, klaus validates the JWT `sub` or `email` claim against this value and returns HTTP 403 for non-matching users.

When empty, owner validation is skipped (backward-compatible).

## Token forwarding via muster

When klaus instances are accessed through muster with SSO token forwarding, the forwarded token is validated against the configured owner subject. No separate OAuth setup is needed on the klaus instance -- muster handles authentication and forwards the token.

## See also

- [HTTP Endpoints reference](../reference/http-endpoints.md)
- [Architecture explanation](../explanation/architecture.md) for how auth fits into the system
