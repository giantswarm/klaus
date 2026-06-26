# VARIANT controls the base distribution: "alpine" (default, ~50 MB) or "slim" (Debian, ~200 MB).
# Both variants produce the same minimal contents: Node.js, Claude Code CLI, ca-certificates, klaus binary.
# Declared before the first FROM so it is available to all FROM instructions.
ARG VARIANT=alpine

# The Go binary is built by CircleCI (architect/go-build) and attached to the
# build context as klaus-<os>-<arch>; this image only assembles the runtime.
# For a local build, produce the binary first (or use `make docker-build`):
#   CGO_ENABLED=0 go build -o klaus-linux-amd64 .

# Go toolchain stage -- lets Claude Code compile and run Go programs inside the container.
FROM golang:1.26.4-alpine AS go-toolchain

# Minimal runtime with Node.js, Claude CLI, and Go toolchain.
FROM node:24-${VARIANT}
ARG VARIANT

# Install ca-certificates (required for TLS).
RUN if [ "$VARIANT" = "alpine" ]; then \
        apk add --no-cache bash ca-certificates; \
    else \
        apt-get update && \
        apt-get install -y --no-install-recommends ca-certificates && \
        rm -rf /var/lib/apt/lists/*; \
    fi

# Install Claude Code CLI globally.
# renovate: datasource=npm depName=@anthropic-ai/claude-code
ARG CLAUDE_CODE_VER=2.1.195
RUN npm install -g @anthropic-ai/claude-code@${CLAUDE_CODE_VER} && \
    npm cache clean --force

# Create a non-root user for running the application.
RUN if [ "$VARIANT" = "alpine" ]; then \
        addgroup -g 1001 klaus && \
        adduser -u 1001 -G klaus -D -s /bin/sh klaus; \
    else \
        groupadd -g 1001 klaus && \
        useradd -u 1001 -g klaus -m -s /bin/sh klaus; \
    fi

# Create workspace directory.
RUN mkdir -p /workspace && chown klaus:klaus /workspace

# Copy the prebuilt Go binary for the target platform.
ARG TARGETOS
ARG TARGETARCH
COPY klaus-${TARGETOS}-${TARGETARCH} /usr/local/bin/klaus

# Copy the Go toolchain so Claude Code can compile and run Go programs.
COPY --from=go-toolchain /usr/local/go /usr/local/go

LABEL io.giantswarm.klaus.type=toolchain \
      io.giantswarm.klaus.name=klaus
USER klaus
WORKDIR /workspace

ENV PORT=8080
ENV SHELL=/bin/bash
ENV GOROOT=/usr/local/go
ENV GOPATH=/workspace/go
ENV PATH="/usr/local/go/bin:${PATH}"
EXPOSE 8080

ENTRYPOINT ["klaus"]
