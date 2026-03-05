# Helm Values

The Helm chart is located at `helm/klaus/`. Below is a summary of the key configuration sections. See [`values.yaml`](../../helm/klaus/values.yaml) for the full defaults.

## Image

```yaml
image:
  name: giantswarm/klaus
  tag: ""          # defaults to chart appVersion
registry:
  domain: gsoci.azurecr.io
port: 8080
```

## Claude configuration

```yaml
claude:
  model: ""                    # e.g. "sonnet", "opus", "haiku"
  maxTurns: 0                  # 0 = unlimited
  permissionMode: "bypassPermissions"
  systemPrompt: ""
  appendSystemPrompt: ""
  maxBudgetUSD: 0              # 0 = no limit
  effort: ""                   # low, medium, high
  fallbackModel: ""
  jsonSchema: ""
  persistentMode: false
  noSessionPersistence: true
  includePartialMessages: false
```

## API key

```yaml
anthropicApiKey:
  secretName: ""              # Kubernetes Secret name
  secretKey: "api-key"        # key within the Secret
```

## Tool control

```yaml
claude:
  tools: []                   # base tool set (empty = all defaults)
  allowedTools: []            # e.g. ["Bash(git:*)", "Edit"]
  disallowedTools: []         # e.g. ["Bash(rm:*)"]
```

## Extensions

```yaml
claude:
  skills: {}                  # inline SKILL.md definitions
  agentFiles: {}              # markdown agent definitions
  agents: {}                  # JSON agent definitions (highest priority)
  activeAgent: ""             # top-level agent selection
  hooks: {}                   # lifecycle hooks
  hookScripts: {}             # hook script contents
  plugins: []                 # OCI plugin references
  pluginDirs: []              # additional plugin directories
  addDirs: []                 # additional .claude/ directories
  loadAdditionalDirsMemory: true
```

## MCP servers

```yaml
claude:
  mcpConfig: ""               # inline JSON MCP config
  strictMcpConfig: true       # only use servers from mcpConfig
```

## Settings

```yaml
claude:
  settingsFile: ""            # path to settings JSON (mutually exclusive with hooks)
  settingSources: ""          # comma-separated: "user,project,local"
```

## Workspace

```yaml
workspace:
  enabled: false
  storageClass: ""
  size: 1Gi
  existingClaim: ""
```

## Telemetry

```yaml
telemetry:
  enabled: false
  metricsExporter: "otlp"
  logsExporter: "otlp"
  otlp:
    protocol: "grpc"
    endpoint: ""
  scrapeAnnotations: false
  serviceMonitor:
    enabled: false
    interval: "30s"
```

## Resources and security

```yaml
resources:
  limits:
    cpu: "2"
    memory: 2Gi
  requests:
    cpu: 250m
    memory: 512Mi

podSecurityContext:
  runAsNonRoot: true
  runAsUser: 1000
  seccompProfile:
    type: RuntimeDefault
```
