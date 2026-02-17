# WASI/WebAssembly Integration Architecture

> **Status:** Proposal
> **Jira:** SRVOCF-750
> **Target:** func CLI v0.37.0+

## Overview

### Why WASI Support?

The func CLI currently builds container images and deploys them as Knative Services or Kubernetes Deployments. This proposal adds an alternative mode that builds **WASI modules** (WebAssembly) and deploys them as `WasmModule` custom resources, executed by the [knative-serving-wasm](https://github.com/cardil/knative-serving-wasm) runner.

### Current vs. Future Benefits

The initial implementation provides a foundation for WASI support with modest improvements. The major performance gains come with the future shared runner architecture.

| Aspect | Containers | WASI (Current) | WASI (Future: Shared Runners) |
|--------|------------|----------------|-------------------------------|
| **Module Size** | 50-500 MB | 100 KB - 2 MB | 100 KB - 2 MB |
| **Cold Start** | 2-10 s | 2-5 s | <100 ms |
| **Warm Start** | <10 ms | <10 ms | <10 ms |
| **Sandboxing** | Process isolation | Capability-based | Capability-based |

**Current model (1:1):** Each WasmModule creates a dedicated runner pod. Cold start includes pod scheduling + runner image pull + WASM fetch + compile — comparable to containers.

**Future model (shared runners):** A pool of long-lived runner pods hosts many modules. Cold start becomes just WASM fetch + compile, bypassing the K8s scheduler entirely.

### Immediate Benefits

Even in the 1:1 model, WASI provides:
- **Smaller artifacts** — 100KB-2MB vs. 50-500MB container images
- **Capability-based security** — Fine-grained control over filesystem, network, and environment access
- **Portability** — Universal bytecode runs anywhere with WASI support
- **Foundation for future** — Architecture ready for shared runner optimization

### Scope

This document covers:
1. Build pipeline — Compiling source to WASM and packaging as OCI artifact
2. Deploy pipeline — Creating WasmModule CRs and monitoring status
3. func.yaml schema changes — New fields for WASI configuration
4. Code changes — New `pkg/wasm/` package and integration points

## Architecture Diagrams

### High-Level Flow

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              Developer                                   │
│              func.yaml ──► func build ──► func deploy                    │
└──────────────────────────────────────┬─────────────────┬─────────────────┘
                                       │                 │
                                       ▼                 ▼
┌──────────────────────────────────────┐  ┌───────────────────────────────┐
│          Build Pipeline              │  │        Deploy Pipeline        │
│                                      │  │                               │
│  Source ──► Compiler ──► module.wasm │  │  func.yaml ──► WasmModule CR  │
│                 │                    │  │                    │          │
│                 ▼                    │  │                    ▼          │
│         Push to Registry ──────────────────────────► Runner Pod         │
│                                      │  │              │                │
└──────────────────────────────────────┘  │              ▼                │
                                          │        Execute WASM           │
                                          └───────────────────────────────┘
```

> **Note:** When `runtime` is a WASI variant (e.g., `rust-wasi`, `go-wasi`), the CLI automatically infers `builder: wasm` and `deployer: wasm`. No explicit flags needed.

### Build Pipeline Detail

```
┌──────────────────────────────────────────────────┐
│                  Build Pipeline                  │
├──────────────────────────────────────────────────┤
│                                                  │
│  Source Code                                     │
│       │                                          │
│       ▼                                          │
│  ┌─────────────────────────────────────┐         │
│  │ Language Compiler                   │         │
│  │                                     │         │
│  │  rust-wasi: cargo build             │         │
│  │      --target wasm32-wasip2         │         │
│  │                                     │         │
│  │  go-wasi: tinygo build              │         │
│  │      -target=wasip2                 │         │
│  │                                     │         │
│  │  ... more lang-specific builds ...  │         │
│  └─────────────────────────────────────┘         │
│       │                                          │
│       ▼                                          │
│  module.wasm                                     │
│       │                                          │
│       ▼                                          │
│  Push directly to OCI Registry                   │
│  (modern registries support WASM natively)       │
│                                                  │
└──────────────────────────────────────────────────┘
```

Modern OCI registries support WASM modules natively — no container image wrapping needed. The runner fetches the WASM module directly from the registry.

## WasmModule CRD

The `WasmModule` custom resource is provided by [knative-serving-wasm](https://github.com/cardil/knative-serving-wasm). The func CLI creates and manages these resources.

### Spec Structure

```yaml
apiVersion: wasm.serving.knative.dev/v1alpha1
kind: WasmModule
metadata:
  name: my-function
  namespace: default
spec:
  # REQUIRED: OCI reference to the WASM module
  image: quay.io/myuser/my-wasm-function:latest

  # Command line arguments
  args: ["--verbose"]

  # Environment variables (full K8s EnvVar support)
  env:
    - name: LOG_LEVEL
      value: debug
    - name: DB_PASSWORD
      valueFrom:
        secretKeyRef:
          name: db-credentials
          key: password

  # WASI network permissions (disabled by default)
  network:
    allowIpNameLookup: true     # DNS resolution
    tcp:
      connect:                  # Outbound connections (most common use case)
        - "db:5432"
        - "db-default.svc:5432"
        - "db-default.svc.cluster.local:5432"
      # bind: []               # Rarely needed - HTTP handled by runner

  # Resource limits
  resources:
    requests:
      memory: "128Mi"
    limits:
      memory: "256Mi"
      cpu: "100m"               # Converted to fuel units

  # Kubernetes volumes
  volumes:
    - name: config
      configMap:
        name: my-config

  # Mounted as WASI preopened directories
  volumeMounts:
    - name: config
      mountPath: /config
      readOnly: true
```

> **Note:** `tcp.bind` is rarely needed — WASM modules are registered as HTTP handlers by the runner, which handles incoming connections. Use `tcp.connect` for outbound connections to databases, APIs, etc.

### Field Mapping from func.yaml

| func.yaml Path | WasmModule Field | Notes |
|----------------|------------------|-------|
| `name` | `metadata.name` | Function name |
| `registry` + `name` | `spec.image` | Full OCI reference |
| `deploy.namespace` | `metadata.namespace` | Target namespace |
| `run.args` | `spec.args` | Command line arguments |
| `run.envs` | `spec.env` | Environment variables |
| `run.volumes` | `spec.volumes` + `spec.volumeMounts` | Volume configuration |
| `deploy.network` | `spec.network` | WASI network permissions |
| `deploy.options.resources` | `spec.resources` | Resource limits |
| `deploy.labels` | `metadata.labels` | User labels |
| `deploy.annotations` | `metadata.annotations` | User annotations |

## Build Pipeline

### Supported Runtimes

Languages with WASI Preview 2 (wasip2) support:

| Runtime | Compiler/Tool | Build Command | Maturity |
|---------|---------------|---------------|----------|
| `rust-wasi` | cargo | `cargo build --target wasm32-wasip2 --release` | Tier 2 (stable) |
| `go-wasi` | tinygo | `tinygo build -target=wasip2 -o module.wasm .` | Stable |
| `python-wasi` | componentize-py | `componentize-py -d wit -w world module -o out.wasm` | Stable |
| `js-wasi` | jco | `jco componentize module.js -w wit -o out.wasm` | Stable |
| `c-wasi` | wasi-sdk | `clang --target=wasm32-wasip2 -o module.wasm` | Stable |
| `cpp-wasi` | wasi-sdk | `clang++ --target=wasm32-wasip2 -o module.wasm` | Stable |
| `dotnet-wasi` | .NET WASI SDK | `dotnet build -c Release` | Experimental |
| `swift-wasi` | SwiftWasm | `swift build --triple wasm32-unknown-wasi` | Experimental |

### Runtime Selection

The runtime is specified in [`func.yaml`](../../pkg/functions/function.go) when creating a new function. Users choose the appropriate WASI runtime template during `func create`:

```bash
func create --language rust-wasi my-function
```

This sets `runtime: rust-wasi` in func.yaml, which determines the build toolchain and deployment target.

### Prerequisites

Before building, the builder verifies toolchain availability:

**Rust:**
- `cargo` installed (from rustup.rs)
- `wasm32-wasip2` target: `rustup target add wasm32-wasip2`

**Go:**
- `tinygo` installed (from tinygo.org)

**Python:**
- `componentize-py` installed: `pip install componentize-py`
- WIT definitions for the target world

**JavaScript:**
- `jco` installed: `npm install -g @bytecodealliance/jco`
- `componentize-js` installed: `npm install -g @bytecodealliance/componentize-js`

**C/C++:**
- `wasi-sdk` installed (from github.com/WebAssembly/wasi-sdk)
- Configured in PATH or via `WASI_SDK_PATH` environment variable

### Registry Push

After compilation, the WASM module is pushed directly to the OCI registry:

```
{registry}/{name}:{tag}
```

Modern registries (Quay, GHCR, Docker Hub, Harbor) support WASM natively. The func CLI uses `go-containerregistry` for push operations.

## Deploy Pipeline

### WasmModule Lifecycle

The deployer creates or updates WasmModule CRs based on func.yaml:

1. **Read** func.yaml and built image reference
2. **Map** func.yaml fields to WasmModule spec
3. **Create/Update** WasmModule CR in target namespace
4. **Wait** for Ready condition
5. **Return** deployment result with URL

### Status Conditions

WasmModule provides status conditions for monitoring:

```yaml
status:
  address:
    url: http://my-function.default.svc.cluster.local
  conditions:
    - type: Ready
      status: "True"
      reason: ModuleRunning
    - type: ModuleLoaded
      status: "True"
      reason: CompiledAndCached
```

The deployer waits for `Ready=True` before returning success. On failure, it reports the error condition to the user.

### Error Cases

| Error | Cause | Resolution |
|-------|-------|------------|
| WasmModule CRD not found | knative-serving-wasm not installed | Install the controller |
| Image pull failed | Invalid OCI reference or auth | Verify registry access |
| Module compile failed | Invalid WASM binary | Rebuild with correct target |

## func.yaml Schema

### New Fields

The following fields are added to support WASI:

```yaml
specVersion: 0.36.0
name: my-wasm-function
runtime: rust-wasi                    # WASI runtime identifier
registry: quay.io/myuser

# Inferred from runtime - no need to specify:
# build:
#   builder: wasm
# deploy:
#   deployer: wasm

run:
  args:                               # NEW: command line arguments
    - "--verbose"
  envs:
    - name: LOG_LEVEL
      value: debug
  volumes:
    - secret: my-secret
      path: /secrets

deploy:
  namespace: default
  network:                            # NEW: WASI network permissions
    allowIpNameLookup: true
    tcp:
      connect:
        - "db:5432"
  options:
    resources:
      requests:
        memory: "128Mi"
      limits:
        memory: "256Mi"
```

### Builder/Deployer Inference

When `runtime` is a WASI variant (e.g., `rust-wasi`, `go-wasi`), the CLI infers:
- `build.builder: wasm`
- `deploy.deployer: wasm`

Users do not need to specify these explicitly. Explicit overrides are supported for advanced use cases.

### Compatibility Validation

The CLI validates that runtime, builder, and deployer are compatible:

| Runtime | Valid Builders | Valid Deployers |
|---------|---------------|-----------------|
| `*-wasi` | `wasm` | `wasm` |
| `node`, `python`, etc. | `pack`, `s2i` | `knative`, `raw`, `keda` |

Invalid combinations are rejected with a clear error message:
- `runtime: rust-wasi` + `builder: pack` → Error
- `runtime: node` + `deployer: wasm` → Error

**Future expansion:** The compatibility matrix may grow over time. Examples:
- Cluster-based WASM builds using S2I or Tekton pipelines
- Buildpacks producing WASM artifacts

### Network Configuration

The `deploy.network` field controls WASI socket permissions:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `allowIpNameLookup` | bool | false | Enable DNS resolution |
| `tcp.connect` | []string | [] | Allowed outbound TCP destinations |
| `tcp.bind` | []string | [] | Allowed TCP listen addresses (rarely needed) |
| `udp.connect` | []string | [] | Allowed outbound UDP destinations |
| `udp.bind` | []string | [] | Allowed UDP listen addresses |

Destination patterns support:
- Exact: `"db:5432"`
- Wildcard port: `"api.example.com:*"`
- Wildcard host: `"*:443"`

## Package Structure

A new `pkg/wasm/` package will be created. The exact file organization is left to the implementer, but it must provide implementations of:

| Interface | Purpose |
|-----------|---------|
| `fn.Builder` | Compile source to WASM and push to registry |
| `fn.Deployer` | Create/update WasmModule CRs |
| `fn.Lister` | List WasmModule CRs in namespace |
| `fn.Remover` | Delete WasmModule CRs |
| `fn.Describer` | Return WasmModule status details |

Key considerations:
- Language-specific build logic can be in separate files or use a strategy pattern
- The K8s client for WasmModule CRUD should be abstracted for testability
- Registry push operations should reuse existing `go-containerregistry` patterns

## Integration Points

### Files Requiring Modification

| File | Change |
|------|--------|
| [`pkg/builders/builders.go`](../../pkg/builders/builders.go) | Add `Wasm = "wasm"` constant, update `All()` |
| [`pkg/functions/function.go`](../../pkg/functions/function.go) | Add `Args` to RunSpec, `Network` to DeploySpec, update enums |
| [`cmd/build.go`](../../cmd/build.go) | Add `"wasm"` case to builder switch |
| [`cmd/deploy.go`](../../cmd/deploy.go) | Add `"wasm"` case to deployer switch |
| [`cmd/completion_util.go`](../../cmd/completion_util.go) | Add `"wasm"` to completion lists |
| [`schema/func_yaml-schema.json`](../../schema/func_yaml-schema.json) | Add network config schema, update enums |
| [`go.mod`](../../go.mod) | Add `github.com/cardil/knative-serving-wasm` dependency |

### Codegen

After modifying `pkg/functions/function.go`, regenerate the schema:

```bash
./hack/update-codegen.sh
```

## Future Considerations

### Shared Runner Architecture

The current 1:1 model (one runner pod per WasmModule) has cold starts comparable to containers. The [shared runner architecture](../../.vscode/sources/knative-serving-wasm/docs/design/shared-runner-architecture.md) proposes a pool of long-lived runner pods hosting multiple WASM modules, reducing cold starts to <100ms.

When implemented, func CLI changes are minimal:
- The `WasmModule` CR spec remains unchanged
- The controller handles module placement automatically
- Users may optionally specify a named runner for isolation
