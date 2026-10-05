# OpenKCM: CMK to Krypton Bridge

[![REUSE status](https://api.reuse.software/badge/github.com/openkcm/cmk-krypton-bridge)](https://api.reuse.software/info/github.com/openkcm/cmk-krypton-bridge)

CMK to Krypton Bridge connects OpenKCM's Customer-Managed Key (CMK) layer with Krypton.
It turns CMK operations such as key creation, rotation, enable/disable and revocation into Krypton-compatible requests,
so customer-controlled keys are enforced the same way in both systems. Part of the OpenKCM ecosystem.

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/openkcm/cmk-krypton-bridge/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/openkcm/cmk-krypton-bridge/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/openkcm/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright (20xx-)20xx SAP SE or an SAP affiliate company and OpenKCM contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/openkcm/cmk-krypton-bridge).


## Code of Conduct

To facilitate a nice environment for all, check out our [Code of Conduct](https://github.com/openkcm/.github/blob/main/CODE_OF_CONDUCT.md).

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
# cmk-krypton-bridge

POC service that sits between the CMK backend (via orbital) and the Krypton key-management backend (via gRPC). It owns the **L1–L4 key hierarchy**: given a tenant/service, it ensures every ancestor key exists before creating the requested one.

---

## Architecture

```
CMK Backend ──orbital──► CmkKryptonBridge ──gRPC──► Krypton Backend
                              │                   (mock in POC)
                         CLI (grpc)
```

### Key hierarchy

| Level | Name | Scope |
|---|---|---|
| L1 | Hardware Root Key | Global, external — read-only in proxy |
| L2 | Tenant Root Key | Per tenant |
| L3 | Service Key | Per tenant + service |
| L4 | Data Encryption Key (DEK) | Per tenant + service |

`EnsureHierarchy` is **recursive and idempotent**: requesting L4 automatically creates L2 and L3 if they are missing. Calling it twice produces no duplicate keys.

Key IDs are **deterministic**:

```
L2  → {tenantID}-l2
L3  → {tenantID}-{serviceID}-l3
L4  → {tenantID}-{serviceID}-l4
```

---

## Repository layout

```
cmd/
  proxy/          daemon entry point
  cli/            cmk2kryptonctl entry point
  demos/          manual integration/demo tools
    krypton-test/ direct Krypton gRPC smoke test
    orbital-demo/ Orbital/AMQP integration demo
internal/
  kryptonclient/  Client interface, in-memory MockClient, gRPC stub
  orchestrator/   EnsureHierarchy logic + unit tests
  server/         gRPC handler, HTTP health endpoints, orbital scaffold
  gen/            generated protobuf code (git-ignored, regenerate with make proto)
pkg/
  config/         Config struct + Viper loader
  logger/         zap logger factory
proto/            .proto source files
charts/           Helm chart scaffold
```

---

## Prerequisites

| Tool | Purpose |
|---|---|
| Go 1.22+ | build & test |
| `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc` | proto code generation |
| `buf` (optional) | alternative proto generator |
| `helm` | lint / package chart |
| `docker` | container build |

Install proto plugins:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

---

## Quick start

```bash
# 1. pull dependencies
go mod tidy

# 2. generate proto code
make proto
# or without buf:
protoc --go_out=internal/gen --go_opt=paths=source_relative \
       --go-grpc_out=internal/gen --go-grpc_opt=paths=source_relative \
       -I proto proto/kms/api/krypton/rpc/v1/proxy_service.proto

# 3. build
make build

# Optional: build demo tools separately
make demo-build

# 4. run (mock mode — no real Krypton needed)
./bin/cmk-krypton-bridge --config=config.yaml
```

The proxy starts two listeners:

- `:9092` — gRPC (inbound from CMK backend / CLI)
- `:8080` — HTTP health (`/healthz`, `/readyz`)

---

## CLI usage

```bash
# health check
./bin/cmk2kryptonctl health

# ensure L3 key (creates L2 automatically if missing)
./bin/cmk2kryptonctl ensure-hierarchy \
  --tenant-id=tenant-abc \
  --service-id=payments \
  --level=L3

# ensure full L4 chain (L2 + L3 + L4 created in one call)
./bin/cmk2kryptonctl ensure-hierarchy \
  --tenant-id=tenant-abc \
  --service-id=payments \
  --level=L4

# list all keys for a tenant
./bin/cmk2kryptonctl list-keys --tenant-id=tenant-abc

# point at a non-default proxy address
./bin/cmk2kryptonctl --addr=proxy.internal:9092 health
```

---

## Configuration

Config is loaded from `config.yaml` then overridden by env vars (`KRYPTON_PROXY_` prefix).

```yaml
grpcServer:
  address: ":9092"

kryptonClient:
  mockMode: true          # POC default — set false to use real Krypton
  address: "krypton:9090"
  secretRef:
    type: insecure        # insecure | mtls (mtls not yet implemented)

orbital:
  enabled: false          # set true when orbital is wired up

keyHierarchy:
  cacheTTL: 30s
  maxDepth: 4
```

Key env var overrides:

| Env var | Config field |
|---|---|
| `KRYPTON_PROXY_KRYPTONCLIENT_MOCKMODE` | `kryptonClient.mockMode` |
| `KRYPTON_PROXY_KRYPTONCLIENT_ADDRESS` | `kryptonClient.address` |
| `KRYPTON_PROXY_GRPCSERVER_ADDRESS` | `grpcServer.address` |
| `KRYPTON_PROXY_LOGGER_LEVEL` | `logger.level` |

---

## Testing

```bash
# unit tests
go test -race ./...

# integration test (full gRPC stack against mock)
go test -tags integration -race ./...
```

The integration test spins up a real gRPC server on a random port, fires `EnsureKeyHierarchy` twice, and asserts idempotency.

---

## Docker

```bash
docker build -t openkcm/cmk-krypton-bridge:dev .
docker run -p 9092:9092 -p 8080:8080 openkcm/cmk-krypton-bridge:dev
```

---

## Helm

```bash
helm lint charts/cmk-krypton-bridge
helm install cmk-krypton-bridge charts/cmk-krypton-bridge \
  --set config.kryptonClient.address=krypton:9090
```

---

## POC limitations / next steps

| Area | Status | Next step |
|---|---|---|
| Krypton gRPC client | stub — returns error | implement once Krypton proto definitions are available |
| Orbital integration | scaffold — `OrbitalHandlerFunc` typedef | replace with `orbital.HandlerFunc` and wire real request/response types |
| mTLS | config wired, not implemented | load cert from `SecretRef` in `grpcclient.go` |
| JWT auth | interceptor hooks commented out | enable `otelgrpc` + JWT interceptor in `NewGRPCServer` |
| Key caching | `cacheTTL` config exists | add in-memory cache layer in `Orchestrator` |
| L1 key ID | hardcoded `l1-global` | make configurable via `config.yaml` |
