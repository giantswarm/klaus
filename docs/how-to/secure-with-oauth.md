# Secure with OAuth

Protect the `/mcp` endpoint with OAuth 2.1 authentication. Klaus supports Dex and Google as OIDC providers via the `mcp-oauth` library.

## Enable OAuth

OAuth is configured via environment variables. The Helm chart wires these from values:

```yaml
# These are passed as environment variables to the klaus container.
# Consult the mcp-oauth library documentation for the full set of options.
```

When OAuth is enabled, the `/mcp` endpoint requires a valid bearer token. Operational endpoints (`/healthz`, `/readyz`, `/status`, `/metrics`) remain unauthenticated.

## Token forwarding via muster

When klaus instances are accessed through muster with SSO token forwarding, muster forwards the user's Dex ID token unchanged. klaus verifies that token itself: its signature against the issuer's JWKS, its issuer, its expiry, and an audience in the allowlist. No OAuth server is needed on the klaus instance.

```yaml
auth:
  tokenIssuerURL: "https://dex.example.com"  # the Dex that issues muster's ID tokens
  tokenAudiences: ["muster"]                 # muster's Dex client ID
```

This sets `KLAUS_TOKEN_ISSUER_URL` and `KLAUS_TOKEN_AUDIENCES` on the container. A request without a valid token gets HTTP 401. muster is not between the caller and the klaus Service, so this check is what protects `/mcp` from any pod that reaches the Service.

## Owner-based access control

Restrict instance access to a specific user by setting the owner subject:

```yaml
owner:
  subject: "user@example.com"  # sub, or email when email_verified is true
```

This sets `KLAUS_OWNER_SUBJECT` on the container. The owner is compared with the verified token only: its `sub`, or its `email` when the token has `email_verified: true`. Other users get HTTP 403. The owner needs `auth.tokenIssuerURL` or OAuth; klaus refuses to start and the chart refuses to render without one.

When empty, any user with a token that klaus accepts is allowed.

## No authentication

Without OAuth and without `auth.tokenIssuerURL`, klaus refuses to start. For an isolated test only, set `auth.allowUnauthenticated: true` (`--allow-unauthenticated`, `KLAUS_ALLOW_UNAUTHENTICATED=true`): then any caller that reaches the port can run the agent.

## See also

- [HTTP Endpoints reference](../reference/http-endpoints.md)
- [Architecture explanation](../explanation/architecture.md) for how auth fits into the system
