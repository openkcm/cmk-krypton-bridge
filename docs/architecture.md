# CmkKryptonBridge — Architecture

## Overview

CmkKryptonBridge is a stateless gRPC service within the **openkcm** ecosystem. Its single responsibility is **key hierarchy orchestration**: given a request for a key at level L2–L4, it ensures every ancestor key exists in Krypton before creating the requested one.

It acts as a translation and orchestration layer between two systems:

- **Inbound**: CMK backend (via orbital sync framework) or direct gRPC callers (CLI, other services)
- **Outbound**: Krypton key-management backend (via gRPC)

```
┌─────────────────────────────────────────────────────────────────┐
│                        openkcm ecosystem                        │
│                                                                 │
│  ┌──────────────┐   orbital/gRPC   ┌─────────────────────────┐ │
│  │ CMK Backend  │ ───────────────► │      CmkKryptonBridge       │ │
│  └──────────────┘                  │                         │ │
│                                    │  ┌───────────────────┐  │ │
│  ┌──────────────┐   gRPC           │  │   gRPC Handler    │  │ │
│  │     CLI      │ ───────────────► │  └────────┬──────────┘  │ │
│  └──────────────┘                  │           │             │ │
│                                    │  ┌────────▼──────────┐  │ │
│                                    │  │   Orchestrator    │  │ │
│                                    │  └────────┬──────────┘  │ │
│                                    │           │             │ │
│                                    │  ┌────────▼──────────┐  │ │
│                                    │  │  KryptonClient    │  │ │
│                                    │  │ (Mock | gRPC)     │  │ │
│                                    │  └────────┬──────────┘  │ │
│                                    └───────────┼─────────────┘ │
│                                                │               │
│                                    ┌───────────▼─────────────┐ │
│                                    │   Krypton Backend       │ │
│                                    └─────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

---

## Key hierarchy model

Krypton organises encryption keys in a strict four-level tree. Each key is cryptographically bound to its parent.

```
L1  ──  Hardware Root Key       (global, external HSM — never created by proxy)
 │
 └── L2  Tenant Root Key        (one per tenant)
          │
          └── L3  Service Key   (one per tenant × service)
                   │
                   └── L4  Data Encryption Key (DEK)
                                               (one per tenant × service)
```

| Level | Created by proxy | Scope | Example ID |
|---|---|---|---|
| L1 | No (external) | Global | `l1-global` |
| L2 | Yes | `{tenantID}` | `acme-l2` |
| L3 | Yes | `{tenantID}-{serviceID}` | `acme-payments-l3` |
| L4 | Yes | `{tenantID}-{serviceID}` | `acme-payments-l4` |

Key IDs are **deterministic** — derived solely from tenant ID, service ID and level. This is what makes `EnsureHierarchy` safe to call multiple times without side effects.

---

## Component breakdown

### gRPC Handler (`internal/server`)

Entry point for all inbound calls. Validates request fields, translates proto types to internal domain types, delegates to the Orchestrator, and maps results back to proto responses.

Exposes four RPCs defined in `proto/kms/api/krypton/rpc/v1/proxy_service.proto`:

| RPC | Description |
|---|---|
| `EnsureKeyHierarchy` | Main operation — ensures key + all ancestors exist |
| `GetKey` | Fetch a single key by ID |
| `ListKeys` | List all keys for a tenant |
| `Health` | Liveness + downstream Krypton status |

Also serves HTTP on `:8080`:

| Path | Purpose |
|---|---|
| `/healthz` | Kubernetes liveness probe |
| `/readyz` | Kubernetes readiness probe (checks Krypton reachability) |

### Orchestrator (`internal/orchestrator`)

Core business logic. Implements the recursive `EnsureHierarchy` algorithm:

```
EnsureHierarchy(tenantID, serviceID, targetLevel):
  keyID = buildKeyID(tenantID, serviceID, targetLevel)

  if GetKey(keyID) succeeds → return existing key   // idempotent fast path

  parentKey = EnsureHierarchy(..., targetLevel - 1)  // recurse upward

  return CreateKey(keyID, parentKey.ID)              // create and link
```

Base cases:
- Level L2: parent is always `l1-global` (must already exist)
- Level L3: parent is L2 of the same tenant
- Level L4: parent is L3 of the same tenant + service

The recursion terminates at L2 because L1 is never created by the proxy.

**Idempotency guarantee**: `buildKeyID` is a pure function — same inputs always produce the same ID. `CreateKey` on the `KryptonClient` interface is defined as create-if-not-exists. Combined, any number of `EnsureHierarchy` calls for the same (tenant, service, level) tuple leaves the system in the same final state.

### KryptonClient (`internal/kryptonclient`)

Interface that abstracts the Krypton backend:

```go
type Client interface {
    GetKey(ctx, id)                  (*Key, error)
    CreateKey(ctx, req)              (*Key, error)
    LinkKey(ctx, childID, parentID)  error
    ListKeysByTenant(ctx, tenantID)  ([]*Key, error)
    Health(ctx)                      error
}
```

Two implementations:

| Implementation | File | Use |
|---|---|---|
| `MockClient` | `mock.go` | POC / testing — in-memory, thread-safe, pre-seeded with `l1-global` |
| `GRPCClient` | `grpcclient.go` | Production — stub, pending Krypton proto definitions |

### Orbital Handler (`internal/server/orbital_handler.go`)

Scaffold for receiving tasks from the CMK backend via the orbital sync framework. In the POC this is a local `HandlerFunc` typedef. When the `github.com/openkcm/orbital` package is available, replace the typedef with `orbital.HandlerFunc` and wire the real `HandlerRequest`/`HandlerResponse` types at the three `TODO` sites.

### Config (`pkg/config`)

Single `Config` struct loaded by Viper. Priority order (highest first):

1. Environment variables (`KRYPTON_PROXY_*`)
2. `config.yaml`
3. Compiled-in defaults

### Logger (`pkg/logger`)

Thin factory around `go.uber.org/zap`. Produces JSON structured logs in production, human-readable console logs when `logger.level=debug`.

---

## Data flow — `EnsureKeyHierarchy` (L4 example, cold start)

```
CLI / CMK Backend
    │
    │  EnsureKeyHierarchyRequest{tenantID="acme", serviceID="billing", level=L4}
    ▼
gRPC Handler
    │  validate fields, map proto → domain
    ▼
Orchestrator.EnsureHierarchy(tenantID, serviceID, L4)
    │
    ├─ GetKey("acme-billing-l4")  → NOT FOUND
    │
    ├─ ensureParent(L4) → EnsureHierarchy(tenantID, serviceID, L3)
    │       ├─ GetKey("acme-billing-l3")  → NOT FOUND
    │       ├─ ensureParent(L3) → EnsureHierarchy(tenantID, "", L2)
    │       │       ├─ GetKey("acme-l2")  → NOT FOUND
    │       │       ├─ ensureParent(L2)   → GetKey("l1-global")  → OK
    │       │       └─ CreateKey("acme-l2", parentID="l1-global")  ✓
    │       └─ CreateKey("acme-billing-l3", parentID="acme-l2")    ✓
    │
    └─ CreateKey("acme-billing-l4", parentID="acme-billing-l3")    ✓
    │
    ▼
EnsureKeyHierarchyResponse{
  requested_key: {id: "acme-billing-l4", level: 4, ...},
  created_keys:  [{acme-l2}, {acme-billing-l3}, {acme-billing-l4}]
}
```

On a **warm** call (all keys already exist), every `GetKey` succeeds immediately and no `CreateKey` is called. `created_keys` is empty.

---

## Deployment

```
Pod: cmk-krypton-bridge
  ├── :9092  gRPC   (CmkKryptonBridgeService)
  └── :8080  HTTP   (/healthz, /readyz)
```

The proxy is stateless — all key state lives in Krypton. Multiple replicas can run concurrently without coordination. Idempotency at the `CreateKey` level (Krypton must honour create-if-not-exists) prevents double-creation under concurrent requests for the same key.

### Kubernetes probes

| Probe | Endpoint | Fails when |
|---|---|---|
| Liveness | `GET /healthz` | Process is unhealthy |
| Readiness | `GET /readyz` | Krypton backend unreachable |

---

## POC vs. production delta

| Concern | POC | Production |
|---|---|---|
| Krypton backend | `MockClient` (in-memory) | `GRPCClient` with generated Krypton stubs |
| Transport security | insecure | mTLS via `SecretRef` |
| Inbound auth | none | JWT interceptor on gRPC server |
| Orbital | local `HandlerFunc` typedef | `orbital.HandlerFunc` + real operator registration |
| Key caching | none (`cacheTTL` config wired) | In-memory cache in `Orchestrator` keyed by key ID |
| L1 key ID | hardcoded `l1-global` | Configurable via `config.yaml` |
| Observability | structured logging only | OpenTelemetry traces + metrics via `otelgrpc` interceptor |
