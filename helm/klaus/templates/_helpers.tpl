{{/*
Expand the name of the chart.
*/}}
{{- define "klaus.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "klaus.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "klaus.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "klaus.labels" -}}
helm.sh/chart: {{ include "klaus.chart" . }}
{{ include "klaus.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
application.giantswarm.io/team: {{ index .Chart.Annotations "application.giantswarm.io/team" | quote }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "klaus.selectorLabels" -}}
app.kubernetes.io/name: {{ include "klaus.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "klaus.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "klaus.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Resolve the container image: toolchainImage takes precedence over the default image.
*/}}
{{- define "klaus.containerImage" -}}
{{- if .Values.toolchainImage -}}
{{ .Values.toolchainImage }}
{{- else -}}
{{ .Values.registry.domain }}/{{ .Values.image.name }}:{{ .Values.image.tag | default .Chart.AppVersion }}
{{- end -}}
{{- end -}}

{{/*
Aggregate CLAUDE_ADD_DIRS from inline skills/agents and user-specified addDirs.
*/}}
{{- define "klaus.addDirs" -}}
{{- $ctx := dict "dirs" (list) -}}
{{- if or .Values.claude.skills .Values.claude.agentFiles -}}
{{- $d := set $ctx "dirs" (append (get $ctx "dirs") "/etc/klaus/extensions") -}}
{{- end -}}
{{- range .Values.claude.addDirs -}}
{{- $d := set $ctx "dirs" (append (get $ctx "dirs") .) -}}
{{- end -}}
{{- join "," (get $ctx "dirs") -}}
{{- end -}}

{{/*
Aggregate CLAUDE_PLUGIN_DIRS from pluginDirs and plugins.
*/}}
{{- define "klaus.pluginDirs" -}}
{{- $ctx := dict "dirs" (list) -}}
{{- range .Values.claude.pluginDirs -}}
{{- $d := set $ctx "dirs" (append (get $ctx "dirs") .) -}}
{{- end -}}
{{- range .Values.claude.plugins -}}
{{- $d := set $ctx "dirs" (append (get $ctx "dirs") (printf "/var/lib/klaus/plugins/%s" (.repository | splitList "/" | last))) -}}
{{- end -}}
{{- join "," (get $ctx "dirs") -}}
{{- end -}}

{{/*
Check if structured MCP servers are defined (non-empty map).
Returns non-empty string when true.
*/}}
{{- define "klaus.hasMcpServers" -}}
{{- if and .Values.claude.mcpServers (ne (toJson .Values.claude.mcpServers) "{}") -}}true{{- end -}}
{{- end -}}

{{/*
Render mcpServers JSON with inferred "type" field. Claude Code requires an
explicit "type" ("http" or "stdio") for each server entry. Without it, HTTP
servers are misidentified as stdio, causing the subprocess to hang.
Entries with a "url" field default to "http"; entries with a "command" field
default to "stdio". Explicit "type" values are preserved.
*/}}
{{- define "klaus.mcpServersJSON" -}}
{{- $enriched := dict -}}
{{- range $name, $server := .Values.claude.mcpServers -}}
  {{- $entry := deepCopy $server -}}
  {{- if not (hasKey $entry "type") -}}
    {{- if hasKey $entry "url" -}}
      {{- $_ := set $entry "type" "http" -}}
    {{- else if hasKey $entry "command" -}}
      {{- $_ := set $entry "type" "stdio" -}}
    {{- end -}}
  {{- end -}}
  {{- $_ := set $enriched $name $entry -}}
{{- end -}}
{{- dict "mcpServers" $enriched | toJson -}}
{{- end -}}

{{/*
Check if a config-scripts volume (mode 0755) is needed for hook scripts.
Returns non-empty string when true.
*/}}
{{- define "klaus.needsScriptsVolume" -}}
{{- if .Values.claude.hookScripts -}}true{{- end -}}
{{- end -}}

{{/*
Validate workspace git settings.
Call once from deployment.yaml; emits nothing on success, fails on error.
*/}}
{{- define "klaus.validateWorkspaceGit" -}}
{{- if and .Values.workspace.gitRepo (not .Values.workspace.enabled) -}}
{{- fail "workspace.gitRepo requires workspace.enabled to be true" -}}
{{- end -}}
{{- if and .Values.workspace.gitSecretName (not .Values.workspace.gitRepo) -}}
{{- fail "workspace.gitSecretName requires workspace.gitRepo to be set" -}}
{{- end -}}
{{- end -}}

{{/*
Validate plugins: each must have tag or digest, and short names must be unique.
Call once from deployment.yaml; emits nothing on success, fails on error.
*/}}
{{- define "klaus.validatePlugins" -}}
{{- $seen := dict -}}
{{- range .Values.claude.plugins -}}
{{- if and (not .tag) (not .digest) -}}
{{- fail (printf "plugin %s requires either tag or digest" .repository) -}}
{{- end -}}
{{- $short := .repository | splitList "/" | last -}}
{{- if hasKey $seen $short -}}
{{- fail (printf "duplicate plugin short name %q (from %s and %s); use unique final path segments" $short (get $seen $short) .repository) -}}
{{- end -}}
{{- $_ := set $seen $short .repository -}}
{{- end -}}
{{- end -}}
