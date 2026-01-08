# NRI Namespace Isolator

[![Build](https://github.com/fulcro-cloud/namespace-isolator/actions/workflows/build.yaml/badge.svg)](https://github.com/fulcro-cloud/namespace-isolator/actions/workflows/build.yaml)
[![Release](https://github.com/fulcro-cloud/namespace-isolator/actions/workflows/release.yaml/badge.svg)](https://github.com/fulcro-cloud/namespace-isolator/actions/workflows/release.yaml)
[![License](https://img.shields.io/badge/License-Apache%202.0%20with%20Commons%20Clause-blue.svg)](LICENSE)

Namespace-level resource isolation for Kubernetes using cgroups v2 and NRI (Node Resource Interface).

## Overview

NRI Namespace Isolator enforces CPU and memory limits at the **namespace level** without requiring individual pods to declare resource limits. All containers in a namespace share a common resource quota managed via systemd cgroup slices.

### How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│                     Kubernetes API                               │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  NamespaceQuota CRD                                       │  │
│  │  namespace: "tenant-a"  cpu: "8"  memory: "16Gi"          │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Node                                                            │
│  ┌──────────────────┐    ┌──────────────────────────────────┐   │
│  │  Agent DaemonSet │───▶│ /sys/fs/cgroup/brasa.slice/      │   │
│  │  (creates cgroups)│    │   brasa-tenant-a.slice/          │   │
│  └──────────────────┘    │     cpu.max: 800000 100000        │   │
│                          │     memory.max: 17179869184       │   │
│  ┌──────────────────┐    └──────────────────────────────────┘   │
│  │  NRI Plugin      │                    ▲                      │
│  │  (routes containers)──────────────────┘                      │
│  └──────────────────┘                                           │
└─────────────────────────────────────────────────────────────────┘
```

## Features

- **Namespace-level quotas**: CPU and memory limits applied to entire namespaces
- **No pod modifications**: Works without requiring resource limits on individual pods
- **Real-time updates**: Change limits without restarting pods
- **Prometheus metrics**: CPU usage, memory usage, throttling, OOM kills
- **Kubernetes events**: Visibility into quota enforcement
- **CEL validation**: Invalid quota values rejected at API level

## Requirements

- Kubernetes 1.25+ (for CEL validation)
- containerd 2.0+ with NRI enabled
- cgroups v2
- systemd (for cgroup management)

## Installation

### Stable Release (Recommended)

```bash
# Install specific version
kubectl apply -f https://github.com/fulcro-cloud/namespace-isolator/releases/download/v1.0.0/install.yaml
```

Check [Releases](https://github.com/fulcro-cloud/namespace-isolator/releases) for available versions.

### Latest (main branch)

```bash
kubectl apply -k https://github.com/fulcro-cloud/namespace-isolator/deploy/kubernetes
```

### From Source

```bash
git clone https://github.com/fulcro-cloud/namespace-isolator.git
cd namespace-isolator
kubectl apply -k deploy/kubernetes/
```

## Container Images

Images are published to GitHub Container Registry:

| Image | Description |
|-------|-------------|
| `ghcr.io/fulcro-cloud/namespace-isolator-agent` | Agent that manages cgroups |
| `ghcr.io/fulcro-cloud/nri-namespace-isolator` | NRI plugin for container routing |

**Tags:**
- `latest` - Latest build from main branch
- `v1.0.0` - Specific release version
- `abc1234` - Specific commit SHA

## Usage

### Create a Namespace Quota

```yaml
apiVersion: brasa.cloud/v1alpha1
kind: NamespaceQuota
metadata:
  name: my-namespace-quota
spec:
  namespace: my-namespace
  cpu: "4"        # 4 vCPUs
  memory: "8Gi"   # 8 GiB
  enabled: true
```

```bash
kubectl apply -f quota.yaml
```

### Check Status

```bash
kubectl get namespacequotas
```

```
NAME                NAMESPACE      CPU   MEMORY   ENABLED   READY   AGE
my-namespace-quota  my-namespace   4     8Gi      true      true    5m
```

### View Events

```bash
kubectl describe namespacequota my-namespace-quota
```

## Metrics

The agent exposes Prometheus metrics on port `9090`:

| Metric | Description |
|--------|-------------|
| `namespace_quota_cpu_usage_usec` | CPU usage in microseconds |
| `namespace_quota_cpu_limit_usec` | CPU limit in microseconds |
| `namespace_quota_cpu_throttled_periods` | Number of throttled periods |
| `namespace_quota_memory_usage_bytes` | Memory usage in bytes |
| `namespace_quota_memory_limit_bytes` | Memory limit in bytes |
| `namespace_quota_oom_kills_total` | Total OOM kills |

## Configuration

### Agent Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--cgroup-root` | `/sys/fs/cgroup` | Cgroup v2 filesystem root |
| `--slice-prefix` | `brasa.slice` | Parent slice name |
| `--metrics-port` | `9090` | Prometheus metrics port |
| `--log-level` | `info` | Log level (debug, info, warn, error) |

### NRI Plugin Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | `namespace-isolator` | NRI plugin name |
| `--idx` | `10` | NRI plugin index |
| `--log-level` | `info` | Log level |

## Development

### Building

```bash
# Build binaries
make build

# Build Docker images
make docker

# Run tests
make test
```

### Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes and commit
4. Push and open a Pull Request

### CI/CD Workflow

| Event | Action | Image Tags |
|-------|--------|------------|
| Pull Request | Run tests | - |
| Push to main | Build + Push | `latest`, `<sha>` |
| Tag `v*` | Build + Push + Release | `v1.0.0`, `latest` |

### Creating a Release

```bash
git tag v1.0.0
git push origin v1.0.0
```

This triggers the release workflow which:
1. Builds and pushes images with version tag
2. Generates `install.yaml` with correct versions
3. Creates a GitHub Release with all artifacts

## Architecture

### Components

- **Agent (DaemonSet)**: Watches NamespaceQuota CRDs, creates/updates cgroup slices via systemd
- **NRI Plugin (DaemonSet)**: Intercepts container creation, routes to namespace cgroup

### Why systemd?

Direct writes to cgroup files are ignored when systemd manages the cgroup hierarchy. The agent uses `nsenter` to execute `systemctl set-property` commands on the host, ensuring limits are properly applied.

## License

Apache License 2.0 with Commons Clause

This software is free to use, modify, and distribute with attribution. However, you may not sell the software itself or offer it as a managed service. See [LICENSE](LICENSE) for details.

Copyright 2025 Fulcro Tecnologia LTDA
