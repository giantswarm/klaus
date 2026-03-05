# Development

## Prerequisites

- Go 1.23+
- Docker
- Helm 3
- kubectl

## Building

```bash
make build
```

## Running locally

```bash
go run main.go
```

## Running tests

```bash
make test
```

## Helm chart

The Helm chart is located in `helm/klaus/`. To lint:

```bash
helm lint helm/klaus/
```

To render templates locally:

```bash
helm template klaus helm/klaus/
```
