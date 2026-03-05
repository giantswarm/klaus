# Deploying to Kubernetes

Deploy klaus to a Kubernetes cluster using the Helm chart.

## Prerequisites

- Kubernetes cluster (1.28+; 1.35+ for OCI image volume plugins)
- Helm 3
- An Anthropic API key stored in a Kubernetes Secret

## Create the API key Secret

```bash
kubectl create namespace klaus
kubectl -n klaus create secret generic anthropic-api-key \
  --from-literal=api-key=$ANTHROPIC_API_KEY
```

## Install the Helm chart

```bash
helm install klaus helm/klaus/ \
  --namespace klaus \
  --set anthropicApiKey.secretName=anthropic-api-key \
  --set claude.model=sonnet
```

## Verify the deployment

```bash
kubectl -n klaus get pods
kubectl -n klaus port-forward svc/klaus 8080:8080

curl http://localhost:8080/healthz   # -> ok
curl http://localhost:8080/status    # -> JSON status
```

## Common configuration

Override defaults via `values.yaml`:

```yaml
claude:
  model: sonnet
  maxTurns: 25
  maxBudgetUSD: 5.0
  appendSystemPrompt: "You are a platform engineer at our company."

anthropicApiKey:
  secretName: anthropic-api-key

workspace:
  enabled: true
  size: 5Gi
```

```bash
helm install klaus helm/klaus/ -n klaus -f values.yaml
```

## Next steps

- [Configure extensions](../how-to/configure-skills.md) (skills, agents, hooks)
- [Set up monitoring](../how-to/set-up-monitoring.md) with Prometheus
- [Secure with OAuth](../how-to/secure-with-oauth.md) for production access control
