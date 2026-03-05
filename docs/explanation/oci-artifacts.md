# OCI Artifacts

Klaus uses OCI artifacts to package and distribute plugins, personalities, and toolchains. This document explains the artifact format, the shared types library, and how each component in the ecosystem produces and consumes these artifacts.

## Artifact types

The Klaus ecosystem defines three artifact types, each with a different packaging strategy:

| Artifact | Purpose | OCI format |
|----------|---------|------------|
| **Plugin** | Bundle of skills, agents, hooks, and MCP servers | Custom ORAS artifact with Klaus-specific media types |
| **Personality** | Bundle of system prompt, plugins, and toolchain image reference | Custom ORAS artifact with Klaus-specific media types |
| **Toolchain** | Container image with language runtimes and build tools | Standard Docker/OCI container image |

Plugins and personalities use custom media types so the kubelet and ORAS clients can distinguish them from regular container images. Toolchains are standard container images because they need to run as the main container (not just provide files).

## Shared types library: klaus-oci

The [`giantswarm/klaus-oci`](https://github.com/giantswarm/klaus-oci) Go module defines the shared constants and types used by all producers and consumers. It is imported by klausctl, klaus-operator, and any CI tooling that builds or inspects Klaus artifacts.

### Media types

```go
// Plugins
MediaTypePluginConfig  = "application/vnd.giantswarm.klaus-plugin.config.v1+json"
MediaTypePluginContent = "application/vnd.giantswarm.klaus-plugin.content.v1.tar+gzip"

// Personalities
MediaTypePersonalityConfig  = "application/vnd.giantswarm.klaus-personality.config.v1+json"
MediaTypePersonalityContent = "application/vnd.giantswarm.klaus-personality.content.v1.tar+gzip"
```

Toolchains use standard Docker/OCI media types -- no custom Klaus media types are needed.

### Manifest annotations

All three artifact types share the same annotation keys for uniform identification:

```go
AnnotationKlausType    = "io.giantswarm.klaus.type"     // "plugin", "personality", or "toolchain"
AnnotationKlausName    = "io.giantswarm.klaus.name"      // human-readable name
AnnotationKlausVersion = "io.giantswarm.klaus.version"   // version string
```

These annotations are set on the OCI manifest (not Docker labels), so consumers can identify artifact type and version with a single manifest GET -- no need to pull config blobs or content layers.

### Metadata structs

Each artifact type has a config blob struct serialized as JSON:

```go
type PluginMeta struct {
    Name        string   `json:"name"`
    Version     string   `json:"version"`
    Description string   `json:"description,omitempty"`
    Skills      []string `json:"skills,omitempty"`
    Commands    []string `json:"commands,omitempty"`
}

type PersonalityMeta struct {
    Name        string `json:"name"`
    Version     string `json:"version"`
    Description string `json:"description,omitempty"`
}

type ToolchainMeta struct {
    Name        string `json:"name"`
    Version     string `json:"version"`
    Description string `json:"description,omitempty"`
}
```

The `ArtifactInfo` struct and `ArtifactInfoFromAnnotations()` helper extract type, name, and version from any manifest's annotation map, providing a uniform way to identify artifacts across types.

### Personality spec

The `PersonalitySpec` type represents the `personality.yaml` file inside a personality artifact:

```go
type PersonalitySpec struct {
    Description string            `yaml:"description,omitempty"`
    Image       string            `yaml:"image,omitempty"`       // toolchain image reference
    Plugins     []PluginReference `yaml:"plugins,omitempty"`     // plugin OCI references
}

type PluginReference struct {
    Repository string `yaml:"repository" json:"repository"`
    Tag        string `yaml:"tag,omitempty" json:"tag,omitempty"`
    Digest     string `yaml:"digest,omitempty" json:"digest,omitempty"`
}
```

A personality ties together a toolchain image and a set of plugins, forming a complete agent configuration that can be referenced by name and version.

## OCI manifest structure

### Plugin and personality artifacts

Both follow the same OCI image manifest layout:

```
OCI Image Manifest (schemaVersion: 2)
├── Config blob   → PluginMeta or PersonalityMeta (JSON)
├── Content layer → Directory tree (tar+gzip)
└── Annotations   → io.giantswarm.klaus.{type,name,version}
```

The config blob uses the artifact-specific media type (`MediaTypePluginConfig` or `MediaTypePersonalityConfig`). The content layer uses the corresponding content media type and contains a flat tar.gz of the artifact directory.

### Toolchain images

Toolchains are standard multi-stage Docker images that extend a Klaus base image (`giantswarm/klaus` or `giantswarm/klaus-debian`) with language-specific runtimes and tools. They use standard OCI image media types. Klaus-specific metadata is set via manifest annotations (using `docker buildx --annotation`) rather than Docker labels, because annotations can be read with a single manifest GET without pulling the config blob.

## Plugin directory structure

A plugin follows the [Claude Code plugin format](https://code.claude.com/docs/en/plugins):

```
my-plugin/
  .claude-plugin/
    plugin.json          # plugin manifest (name, description, version)
  skills/
    <name>/SKILL.md      # domain knowledge for the agent
  agents/
    <name>.md            # subagent definitions
  hooks/
    hooks.json           # lifecycle hook configuration
    <script>.sh          # hook scripts (executable)
  .mcp.json              # MCP server definitions
```

All files are optional. A plugin can contain any subset of these extension types.

## Ecosystem roles

### klausctl (producer and consumer)

[klausctl](https://github.com/giantswarm/klausctl) is the CLI for local development. It acts as both a producer and consumer of OCI artifacts.

**Pushing plugins:**

```bash
klausctl plugin push gsoci.azurecr.io/giantswarm/klaus-plugins/my-plugin:v1.0.0
```

klausctl packages the current plugin directory into an OCI artifact (config blob + tar.gz layer), sets annotations, and pushes to the registry using ORAS.

**Pulling plugins:**

During `klausctl start`, plugins declared in `~/.config/klausctl/config.yaml` are pulled via ORAS to `~/.config/klausctl/plugins/<name>/`. Each plugin directory is then bind-mounted read-only into the container at `/var/lib/klaus/plugins/<name>`.

**Digest-based caching:**

Each pulled plugin stores a `.klausctl-cache.json` file containing the manifest digest and pull timestamp. On subsequent pulls, klausctl resolves the tag to a digest and skips re-pulling if the digest matches the cached value. This correctly handles mutable tags.

**Toolchain images:**

klausctl scaffolds toolchain Dockerfiles (`klausctl toolchain init`) and builds them as standard container images. Toolchains are not OCI plugin artifacts -- they are regular Docker images used as the `image` in the klausctl config.

**Authentication:**

klausctl resolves registry credentials from (in order):
1. `KLAUSCTL_REGISTRY_AUTH` environment variable (base64-encoded Docker config JSON)
2. `~/.docker/config.json` (Docker credential store)
3. `$XDG_RUNTIME_DIR/containers/auth.json` (Podman credential store)
4. Anonymous access (fallback for public registries)

**Config example:**

```yaml
# ~/.config/klausctl/config.yaml
image: gsoci.azurecr.io/giantswarm/klaus-go:v1.0.0   # toolchain image
plugins:
  - repository: gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base
    tag: v0.6.0
  - repository: gsoci.azurecr.io/giantswarm/klaus-plugins/security
    digest: sha256:abc123...
```

### Helm chart (consumer)

The [Helm chart](../reference/helm-values.md) (`helm/klaus/`) deploys klaus to Kubernetes. It consumes OCI artifacts using native Kubernetes OCI image volumes ([KEP-4639](https://github.com/kubernetes/enhancements/issues/4639)).

**Plugin delivery:**

Plugins declared in `values.yaml` are mounted as Kubernetes image volumes -- the kubelet pulls and caches the OCI artifact directly, with no init containers or ORAS client needed:

```yaml
# values.yaml
claude:
  plugins:
    - repository: gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base
      tag: v0.6.0
```

This generates a volume spec and volume mount in the Deployment:

```yaml
# Generated volume (in deployment.yaml)
volumes:
  - name: plugin-gs-base
    image:
      reference: gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base:v0.6.0
      pullPolicy: IfNotPresent

# Generated volume mount
volumeMounts:
  - name: plugin-gs-base
    mountPath: /var/lib/klaus/plugins/gs-base
    readOnly: true
```

The `klaus.pluginDirs` helper template aggregates all plugin mount paths and explicit `pluginDirs` entries into the `CLAUDE_PLUGIN_DIRS` environment variable. The `klaus.validatePlugins` helper ensures each plugin has a `tag` or `digest`, and that short names (last path segment) are unique.

**Toolchain images:**

Set `toolchainImage` to override the default container image with a composite toolchain image:

```yaml
toolchainImage: gsoci.azurecr.io/giantswarm/klaus-go:1.25
```

The `klaus.containerImage` helper template resolves `toolchainImage` (if set) or falls back to `registry.domain/image.name:image.tag`.

**Inline extensions:**

The Helm chart also supports inline skills, agents, hooks, and MCP servers via `values.yaml`, rendered into a ConfigMap and mounted as files. These are complementary to plugins -- inline extensions are useful for instance-specific configuration, while plugins are reusable packages.

### klaus-operator (consumer)

The [klaus-operator](https://github.com/giantswarm/klaus-operator) manages klaus instances dynamically via CRDs. It consumes the same OCI artifacts as the Helm chart but with a different configuration source.

**CRDs:**

- `KlausInstance` -- declares a klaus deployment with plugins, toolchain image, and extension configuration
- `KlausPersonality` -- reusable template that bundles a toolchain image, plugins, and personality settings

The operator controller renders `KlausInstance` specs into Deployment manifests with the same OCI image volumes, environment variables, and file mounts as the Helm chart. `KlausPersonality` CRDs reference a personality OCI artifact or define the configuration inline, allowing multiple instances to share the same agent configuration.

**Plugin delivery:**

The operator renders OCI image volumes in the managed Deployment spec, identical to the Helm chart approach. The kubelet handles pulling and caching.

### Comparison of delivery mechanisms

| Aspect | klausctl (local) | Helm chart (K8s) | Operator (K8s) |
|--------|-----------------|------------------|----------------|
| **Plugin delivery** | ORAS client pulls to `~/.config/klausctl/plugins/` | Kubernetes OCI image volumes (KEP-4639) | Kubernetes OCI image volumes (KEP-4639) |
| **Toolchain delivery** | Docker/Podman image pull (standard) | Container image in Deployment spec | Container image in Deployment spec |
| **Plugin mount path** | `/var/lib/klaus/plugins/<name>` | `/var/lib/klaus/plugins/<name>` | `/var/lib/klaus/plugins/<name>` |
| **Env var** | `CLAUDE_PLUGIN_DIRS` | `CLAUDE_PLUGIN_DIRS` | `CLAUDE_PLUGIN_DIRS` |
| **Config source** | `~/.config/klausctl/config.yaml` | `values.yaml` | `KlausInstance` / `KlausPersonality` CRDs |
| **Caching** | Digest-based (`.klausctl-cache.json`) | kubelet image cache (`IfNotPresent`) | kubelet image cache (`IfNotPresent`) |
| **Auth** | Docker/Podman credential stores | Kubernetes image pull secrets | Kubernetes image pull secrets |

Klaus itself is delivery-agnostic -- it reads plugin directories from `CLAUDE_PLUGIN_DIRS` regardless of how they were populated.

## Publishing artifacts

### Publishing a plugin with klausctl

```bash
cd my-plugin/
klausctl plugin push gsoci.azurecr.io/giantswarm/klaus-plugins/my-plugin:v1.0.0
```

### Publishing a plugin with oras CLI

```bash
tar czf plugin.tar.gz -C ./my-plugin .

oras push gsoci.azurecr.io/giantswarm/klaus-plugins/my-plugin:v1.0.0 \
  --config config.json:application/vnd.giantswarm.klaus-plugin.config.v1+json \
  plugin.tar.gz:application/vnd.giantswarm.klaus-plugin.content.v1.tar+gzip
```

Where `config.json` contains a `PluginMeta` JSON object:

```json
{
  "name": "my-plugin",
  "version": "v1.0.0",
  "description": "My Klaus plugin",
  "skills": ["k8s", "security"],
  "commands": []
}
```

### Building a toolchain image

```bash
klausctl toolchain init --name my-toolchain
# Edit the generated Dockerfile to add your language runtime
klausctl toolchain build
klausctl toolchain push gsoci.azurecr.io/giantswarm/klaus-my-toolchain:v1.0.0
```

Or build directly with Docker, extending the Klaus base image:

```dockerfile
FROM gsoci.azurecr.io/giantswarm/klaus:latest
RUN apk add --no-cache go git make
```

Set Klaus annotations at build time for discoverability:

```bash
docker buildx build \
  --annotation "manifest:io.giantswarm.klaus.type=toolchain" \
  --annotation "manifest:io.giantswarm.klaus.name=my-toolchain" \
  --annotation "manifest:io.giantswarm.klaus.version=v1.0.0" \
  -t gsoci.azurecr.io/giantswarm/klaus-my-toolchain:v1.0.0 .
```

## Registry considerations

Neither ACR nor the OCI Distribution Spec supports server-side search or filtering by annotations. Discovery of Klaus artifacts relies on convention-based repository naming:

- **Plugins:** `<registry>/giantswarm/klaus-plugins/<name>`
- **Toolchains:** `<registry>/giantswarm/klaus-<toolchain-name>`
- **Personalities:** `<registry>/giantswarm/klaus-personalities/<name>`

After discovery via repository listing and tag enumeration, manifest annotations provide the cheapest way to extract metadata -- a single GET to the manifest endpoint, with no need to pull content layers or config blobs.

## See also

- [Use Plugins](../how-to/use-plugins.md) -- how-to guide for plugin configuration
- [Extension System](extension-system.md) -- how all extension types reach Claude Code
- [Architecture](architecture.md) -- overall system architecture
- [Helm Values reference](../reference/helm-values.md) -- complete chart configuration
- [`giantswarm/klaus-oci`](https://github.com/giantswarm/klaus-oci) -- shared Go types library
- [`giantswarm/klausctl`](https://github.com/giantswarm/klausctl) -- CLI for local development
- [`giantswarm/klaus-operator`](https://github.com/giantswarm/klaus-operator) -- Kubernetes operator
