# klaus

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Go wrapper around claude-code to orchestrate AI agents within Kubernetes

**Homepage:** <https://github.com/giantswarm/klaus>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| giantswarm/team-planeteers | <team-planeteers@giantswarm.io> |  |

## Source Code

* <https://github.com/giantswarm/klaus>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| replicaCount | int | `1` |  |
| image.name | string | `"giantswarm/klaus"` |  |
| image.tag | string | `""` |  |
| registry.domain | string | `"gsoci.azurecr.io"` |  |
| port | int | `8080` |  |
| toolchainImage | string | `""` |  |
| claude.model | string | `""` |  |
| claude.maxTurns | int | `0` |  |
| claude.permissionMode | string | `"bypassPermissions"` |  |
| claude.systemPrompt | string | `""` |  |
| claude.appendSystemPrompt | string | `""` |  |
| claude.mcpConfig | string | `""` |  |
| claude.mcpServers | object | `{}` |  |
| claude.mcpServerSecrets | list | `[]` |  |
| claude.mcpTimeout | int | `0` |  |
| claude.maxMcpOutputTokens | int | `0` |  |
| claude.strictMcpConfig | bool | `true` |  |
| claude.maxBudgetUSD | int | `0` |  |
| claude.effort | string | `""` |  |
| claude.fallbackModel | string | `""` |  |
| claude.jsonSchema | string | `""` |  |
| claude.settingsFile | string | `""` |  |
| claude.settingSources | string | `""` |  |
| claude.tools | list | `[]` |  |
| claude.allowedTools | list | `[]` |  |
| claude.disallowedTools | list | `[]` |  |
| claude.pluginDirs | list | `[]` |  |
| claude.addDirs | list | `[]` |  |
| claude.loadAdditionalDirsMemory | bool | `true` |  |
| claude.skills | object | `{}` |  |
| claude.agentFiles | object | `{}` |  |
| claude.hooks | object | `{}` |  |
| claude.hookScripts | object | `{}` |  |
| claude.plugins | list | `[]` |  |
| claude.agents | object | `{}` |  |
| claude.activeAgent | string | `""` |  |
| claude.includePartialMessages | bool | `false` |  |
| claude.mode | string | `"agent"` |  |
| owner.subject | string | `""` |  |
| anthropicApiKey.secretName | string | `""` |  |
| anthropicApiKey.secretKey | string | `"api-key"` |  |
| workspace.enabled | bool | `false` |  |
| workspace.storageClass | string | `""` |  |
| workspace.size | string | `"1Gi"` |  |
| workspace.existingClaim | string | `""` |  |
| workspace.gitRepo | string | `""` |  |
| workspace.gitRef | string | `""` |  |
| workspace.gitDepth | int | `0` |  |
| workspace.gitTimeout | int | `300` |  |
| workspace.gitImage | string | `"alpine/git:v2.54.0"` |  |
| workspace.gitSecretName | string | `""` |  |
| workspace.gitResources | object | `{}` |  |
| telemetry.enabled | bool | `false` |  |
| telemetry.metricsExporter | string | `"otlp"` |  |
| telemetry.logsExporter | string | `"otlp"` |  |
| telemetry.otlp.protocol | string | `"grpc"` |  |
| telemetry.otlp.endpoint | string | `""` |  |
| telemetry.otlp.headers | string | `""` |  |
| telemetry.metricExportIntervalMs | int | `60000` |  |
| telemetry.logsExportIntervalMs | int | `5000` |  |
| telemetry.logUserPrompts | bool | `false` |  |
| telemetry.logToolDetails | bool | `false` |  |
| telemetry.includeSessionId | bool | `true` |  |
| telemetry.includeVersion | bool | `false` |  |
| telemetry.includeAccountUuid | bool | `true` |  |
| telemetry.resourceAttributes | string | `""` |  |
| telemetry.scrapeAnnotations | bool | `false` |  |
| telemetry.serviceMonitor.enabled | bool | `false` |  |
| telemetry.serviceMonitor.interval | string | `"30s"` |  |
| telemetry.serviceMonitor.scrapeTimeout | string | `"10s"` |  |
| telemetry.serviceMonitor.namespaceSelector | object | `{}` |  |
| resources.limits.cpu | string | `"2"` |  |
| resources.limits.memory | string | `"2Gi"` |  |
| resources.requests.cpu | string | `"250m"` |  |
| resources.requests.memory | string | `"512Mi"` |  |
| podSecurityContext.runAsNonRoot | bool | `true` |  |
| podSecurityContext.runAsUser | int | `1001` |  |
| podSecurityContext.runAsGroup | int | `1001` |  |
| podSecurityContext.fsGroup | int | `1001` |  |
| podSecurityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| securityContext.allowPrivilegeEscalation | bool | `false` |  |
| securityContext.readOnlyRootFilesystem | bool | `true` |  |
| securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| serviceAccount.create | bool | `true` |  |
| serviceAccount.name | string | `""` |  |
| serviceAccount.annotations | object | `{}` |  |
| nodeSelector | object | `{}` |  |
| tolerations | list | `[]` |  |
| affinity | object | `{}` |  |
| global.podSecurityStandards.enforced | bool | `false` |  |
