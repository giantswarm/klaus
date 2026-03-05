# Set Up Monitoring

Klaus exposes Prometheus metrics at `/metrics` and supports OpenTelemetry pass-through for the Claude Code subprocess.

## Prometheus metrics

The `/metrics` endpoint is always available, regardless of OpenTelemetry configuration.

### Available metrics

| Metric | Type | Description |
|--------|------|-------------|
| `klaus_prompts_total` | Counter | Total prompts processed |
| `klaus_prompt_duration_seconds` | Histogram | Prompt execution duration |
| `klaus_process_status` | Gauge | Current process status |
| `klaus_session_cost_usd_total` | Gauge | Cumulative session cost in USD |
| `klaus_messages_total` | Counter | Total messages processed |
| `klaus_tool_calls_total` | Counter | Total tool calls made |
| `klaus_process_restarts_total` | Counter | Process restart count (persistent mode) |

### Scrape annotations

Enable Prometheus scrape annotations on the Service:

```yaml
telemetry:
  scrapeAnnotations: true
```

### ServiceMonitor

For Prometheus Operator:

```yaml
telemetry:
  serviceMonitor:
    enabled: true
    interval: "30s"
    scrapeTimeout: "10s"
```

## OpenTelemetry pass-through

Pass `OTEL_*` environment variables to the Claude Code subprocess for its native telemetry (tokens, cost, sessions, tool events):

```yaml
telemetry:
  enabled: true
  metricsExporter: "otlp"    # otlp, prometheus, console
  logsExporter: "otlp"       # otlp, console
  otlp:
    protocol: "grpc"
    endpoint: "otel-collector.monitoring:4317"
  metricExportIntervalMs: 60000
  logsExportIntervalMs: 5000
```

### Privacy controls

```yaml
telemetry:
  logUserPrompts: false      # don't log prompt contents
  logToolDetails: false      # don't log tool arguments
  includeSessionId: true
  includeAccountUuid: true
```

### Resource attributes

Add custom resource attributes for team identification (W3C Baggage format):

```yaml
telemetry:
  resourceAttributes: "department=engineering,team.id=platform"
```

## See also

- [HTTP Endpoints reference](../reference/http-endpoints.md)
- [Claude Code Monitoring docs](https://code.claude.com/docs/en/monitoring-usage)
