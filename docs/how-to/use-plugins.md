# Use Plugins

Plugins are the composition unit that bundles skills, agents, hooks, and MCP servers into a single distributable package. They are stored as OCI artifacts in container registries and delivered to klaus via Kubernetes image volumes or ORAS.

## OCI plugins in Helm values

Reference plugins from an OCI registry. On Kubernetes 1.35+, these are mounted as native image volumes -- the kubelet handles pulling and caching:

```yaml
claude:
  plugins:
    - repository: gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base
      tag: v0.6.0
    - repository: gsoci.azurecr.io/giantswarm/klaus-plugins/security
      digest: sha256:abc123...
```

Each plugin is mounted at `/var/lib/klaus/plugins/<name>` and added to `CLAUDE_PLUGIN_DIRS`.

## Plugin directory structure

A Klaus plugin follows the [Claude Code plugin format](https://code.claude.com/docs/en/plugins):

```
my-plugin/
  .claude-plugin/
    plugin.json          # plugin manifest
  skills/
    <name>/SKILL.md      # skills
  agents/
    <name>.md            # subagent definitions
  hooks/
    hooks.json           # lifecycle hooks
    <script>.sh          # hook scripts
  .mcp.json              # MCP server config
```

## OCI artifact format

Plugins are packaged as OCI artifacts with custom media types:

- **Config:** `application/vnd.giantswarm.klaus-plugin.config.v1+json`
- **Layer:** `application/vnd.giantswarm.klaus-plugin.content.v1.tar+gzip`

## Publishing a plugin

Using the `oras` CLI:

```bash
tar czf plugin.tar.gz -C ./my-plugin .

oras push gsoci.azurecr.io/giantswarm/klaus-plugins/my-plugin:v1.0.0 \
  --config config.json:application/vnd.giantswarm.klaus-plugin.config.v1+json \
  plugin.tar.gz:application/vnd.giantswarm.klaus-plugin.content.v1.tar+gzip
```

## Plugin delivery across modes

| Mode | Delivery mechanism |
|------|-------------------|
| **Helm chart** | Kubernetes OCI image volumes (KEP-4639) |
| **Operator** | Operator renders image volumes on managed pods |
| **Local (klausctl)** | ORAS client pulls to `~/.config/klausctl/plugins/` |

Klaus itself is delivery-agnostic -- it reads plugin directories from `CLAUDE_PLUGIN_DIRS` regardless of how they were populated.

## Additional plugin directories

Mount custom plugin directories:

```yaml
claude:
  pluginDirs:
    - /mnt/custom-plugins
```

## See also

- [OCI Artifacts explanation](../explanation/oci-artifacts.md) -- format, shared types, and ecosystem roles
- [Extension System explanation](../explanation/extension-system.md)
- [Claude Code Plugins docs](https://code.claude.com/docs/en/plugins)
