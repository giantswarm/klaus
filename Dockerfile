# Stage 1: Build the Go binary.
FROM golang:1.23 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG BUILDTIMESTAMP=unknown
ARG GITSHA=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags "-w -extldflags '-static' \
    -X 'main.version=${VERSION}' \
    -X 'github.com/giantswarm/klaus/pkg/project.buildTimestamp=${BUILDTIMESTAMP}' \
    -X 'github.com/giantswarm/klaus/pkg/project.gitSHA=${GITSHA}'" \
    -o klaus .

# Stage 2: Runtime with Node.js and Claude CLI.
FROM node:22-slim

# Install system dependencies needed by Claude Code.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        git \
        openssh-client \
        curl \
    && rm -rf /var/lib/apt/lists/*

# Install Claude Code CLI globally.
RUN npm install -g @anthropic-ai/claude-code && \
    npm cache clean --force

# Create a non-root user for running the application.
RUN groupadd -g 1000 klaus && \
    useradd -u 1000 -g klaus -m -s /bin/bash klaus

# Create workspace directory.
RUN mkdir -p /workspace && chown klaus:klaus /workspace

# Copy the Go binary from the builder stage.
COPY --from=builder /app/klaus /usr/local/bin/klaus

USER klaus
WORKDIR /workspace

ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["klaus"]
