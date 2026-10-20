# CMK-2-Krypton Bridge Helm chart

The chart `cmkkryptonbridge` deploys the CMK-2-Krypton Bridge, which connects
[CMK](https://github.com/openkcm/cmk) to [Krypton](https://github.com/openkcm/krypton) for one
region. The bridge receives CMK's key and system tasks of its region from an AMQP queue,
creates the matching key hierarchies in the Krypton root node, keeps its jobs in PostgreSQL,
registers the tenants and systems in the CMK Registry, and answers CMK on an AMQP exchange.

> [!WARNING]
> The bridge is in development. The chart deploys the release of its `appVersion` by default,
> the image `ghcr.io/openkcm/images/cmk-krypton-bridge` (linux/amd64 and linux/arm64). To
> deploy an image of the Publish Demo workflow instead (amd64 only), set `image.repository` to
> `cmk-krypton-bridge/images/cmk-krypton-bridge` and `image.tag` to its tag, for example
> `demo-e784835`.

[values.yaml](values.yaml) documents every value and the whole bridge configuration. The
[OpenKCM documentation](https://openkcm.github.io/components/cmk/krypton-bridge) describes how
the bridge and CMK exchange tasks.

## What the chart creates

| Resource | Purpose |
| --- | --- |
| Deployment | The bridge pods |
| ConfigMap | The bridge configuration, rendered from `config` and mounted as `/etc/cmk-krypton-bridge/config.yaml` |
| Service | The gRPC API (9092) and the health endpoints (8080) |
| ServiceAccount | Identity of the pods. Its token is not mounted: the bridge does not call the Kubernetes API |
| NetworkPolicy | Optional (`networkPolicy.enabled`): who may reach the pods |
| PodDisruptionBudget | Optional (`pod.disruptionBudget.enabled`) |
| HorizontalPodAutoscaler | Optional (`hpa.enabled`) |

The resources are named `<release name>-cmk-krypton-bridge`, or `fullnameOverride`.

## Prerequisites

- Kubernetes 1.23 or later, Helm 3.
- The Krypton root node (chart `krypton-root`), reachable at `config.kryptonClient.address`.
- PostgreSQL. The bridge creates its tables at startup, so its database user needs to own the
  database or have `CREATE` on its schema.
- The AMQP broker that CMK uses, with the queue of the bridge's region
  (`/queues/krypton-bridge.<region>.requests`) and a queue for CMK bound to the answers exchange.
  The bridge connects with a client certificate (SASL EXTERNAL) by default.
- The CMK Registry, if `config.registry.enabled` (default).

## Install

Create the Secrets with the PostgreSQL connection string and the AMQP client certificate:

```sh
kubectl -n services create secret generic cmk-krypton-bridge-db \
  --from-literal=DATABASE_DSN='postgres://bridge:<password>@postgresql.services:5432/cmk_krypton_bridge?sslmode=verify-full'
kubectl -n services create secret generic cmk-krypton-bridge-rabbitmq-tls \
  --from-file=tls.crt=bridge.crt --from-file=tls.key=bridge.key --from-file=ca.crt=rabbitmq-ca.crt
```

Create a values file, for example `cmk-krypton-bridge.yaml`, for the region `eu10`:

```yaml
database:
  existingSecret: cmk-krypton-bridge-db
rabbitmq:
  existingSecret: cmk-krypton-bridge-rabbitmq-tls

networkPolicy:
  enabled: true
  http:
    from:
      - namespaceSelector:
          matchLabels: { kubernetes.io/metadata.name: monitoring }

config:
  kryptonClient:
    address: krypton-root.services.svc.cluster.local:8080
  orbital:
    amqp:
      url: amqps://rabbitmq.services.svc.cluster.local:5671
      source: /queues/krypton-bridge.eu10.requests
      target: /exchanges/cmk/eu10.krypton-bridge.replies
  registry:
    address: registry.services.svc.cluster.local:9092
    region: eu10
    ownerId: krypton-bridge
    systemType: krypton-bridge
```

Install the chart:

```sh
helm install cmk-krypton-bridge oci://ghcr.io/openkcm/charts/cmkkryptonbridge \
  --version 0.2.0 -n services -f cmk-krypton-bridge.yaml
```

The release notes show the addresses and warn about values with which the bridge would not
start, not connect or not register its systems. The pod is ready once it reaches Krypton:

```sh
kubectl -n services port-forward service/cmk-krypton-bridge-cmk-krypton-bridge 8080:http
curl -s http://localhost:8080/readyz
```

## Ports

| Port | Serves |
| --- | --- |
| `grpc` (9092) | `CmkKryptonBridgeService`: create key hierarchies and query jobs, used by `cmk2kryptonctl`. Plaintext, without authentication |
| `http` (8080) | `/healthz` (process) and `/readyz` (connection to Krypton) |

## Important values

| Value | Default | Description |
| --- | --- | --- |
| `image.tag` / `image.digest` | chart's `appVersion` | The bridge image to deploy, from `ghcr.io/openkcm/images/cmk-krypton-bridge` |
| `database.existingSecret` | `""` | Secret with the PostgreSQL connection string (key `DATABASE_DSN`). Required |
| `rabbitmq.existingSecret` | `""` | Secret with the AMQP client certificate. Required with mutual TLS (default) |
| `config.kryptonClient` | `krypton-root.services.svc.cluster.local:8080`, plaintext | The Krypton root node |
| `config.orbital.amqp` | `cf-eu10` queue and exchange | The AMQP broker, queue and exchange of the region |
| `config.registry` | enabled, plaintext, `ownerId` and `systemType` empty | Registration in the CMK Registry; set `ownerId` and `systemType` |
| `networkPolicy.enabled` | `false` | Restrict who may reach the gRPC API |
| `resources` | 50m CPU, 64Mi memory and 64Mi ephemeral storage requested; 500m CPU, 256Mi memory and 256Mi ephemeral storage limits | |

## Secrets and environment variables

The bridge reads every setting of `config` also from an environment variable
`KRYPTON_PROXY_<KEY>`: the key in upper case, with `_` for `.`. The chart sets
`KRYPTON_PROXY_DATABASE_DSN` from `database.existingSecret`. For other secrets, such as the
password of `config.orbital.amqp.auth.basic` or `config.registry.bearerToken`, keep the key in
`config` with an empty value and set the variable with `extraEnvs` from a Secret: a variable
takes effect only for keys that are in the configuration or have a built-in default.

## Security defaults

| Setting | Default |
| --- | --- |
| Pod | User and group 65532 (non-root), `RuntimeDefault` seccomp profile, no Service environment variables, no ServiceAccount token |
| Container | No privilege escalation, all capabilities dropped, read-only root filesystem, writable `/tmp` |
| Secrets | From Kubernetes Secrets, as environment variables or mounted files; nothing secret in the ConfigMap |
| Service | `ClusterIP` |

The gRPC API has no authentication. Restrict its callers with `networkPolicy` or a service
mesh, and do not expose it outside the cluster. The connections to Krypton and the Registry
are plaintext by default, for a service mesh with mutual TLS; values.yaml describes TLS and
mutual TLS for both.

## Network policy

With `networkPolicy.enabled: true`, the chart allows ingress only from the peers in
`networkPolicy.grpc.from` (gRPC API) and `networkPolicy.http.from` (health endpoints). An empty
list denies all callers of that port. Outgoing traffic is not restricted. Probes from the
kubelet are not affected on most network plugins.

## Configuration changes

The bridge reads its configuration at startup. The chart adds a `checksum/config` annotation
to the pods, so a configuration change restarts them during `helm upgrade`.

Everything under `config` except `isImmutable` is written to `config.yaml`. The bridge ignores
keys it does not know: check the spelling of the keys against values.yaml.

## Upgrading from 0.1.0

| Change | Action |
| --- | --- |
| Everything under `config` reaches the bridge. Chart 0.1.0 wrote only some keys: `config.registry.systemType`, `config.logger` and the TLS settings were dropped | Check your `config`: keys that had no effect before now apply. Set `config.registry.systemType`, otherwise the Registry rejects the registrations |
| The default image is still `ghcr.io/openkcm/images/cmk-krypton-bridge`, now with the tag from the chart's `appVersion` (`v0.0.1`; was `0.1.0`, which does not exist). `image.registry` (`ghcr.io/openkcm`) is new, and `image.repository` is `images/cmk-krypton-bridge` without the host | If `image.repository` contains the registry host, set `image.registry: ""`; the chart stops with an error otherwise |
| `database.secretName` is now `database.existingSecret`, with the key in `database.dsnKey` (default `DATABASE_DSN`, as before). The inline `database.dsn` was removed | Move your values; the chart stops with an error while the old keys are set |
| `orbital.rabbitmq.secretName` is now `rabbitmq.existingSecret` | Move your value; the chart stops with an error while the old key is set |
| `service.grpcPort` and `service.httpPort` are replaced by the lists `service.ports` and `ports` | Move your values; the chart stops with an error while the old keys are set |
| The ConfigMap is named `<full name>-config`, and the configuration is mounted as the file `/etc/cmk-krypton-bridge/config.yaml` | None |
| New security defaults (group 65532, read-only root filesystem, dropped capabilities, `RuntimeDefault` seccomp profile, no ServiceAccount token, no Service environment variables), a ServiceAccount of the chart, a startup probe, and ephemeral-storage requests and limits | Override `securityContext`, `pod.securityContext`, the probes and `resources` if needed |

Resource names and selector labels are unchanged.

## Troubleshooting

| Symptom | Cause |
| --- | --- |
| Pod exits with `database.dsn is required` | `database.existingSecret` is not set |
| Pod exits with `running migrations` | PostgreSQL is not reachable with the connection string, or the user may not create tables |
| Pod is not ready; `/readyz` answers `not ready` | The bridge cannot reach Krypton at `config.kryptonClient.address` |
| Log `orbital: KryptonReconciler stopped` | The bridge cannot connect to the AMQP broker or open its queue: check `config.orbital.amqp` and the client certificate |
| Log `registry EnsureSystem failed — key created but not registered` | The Registry is not reachable, or rejects the registration: set `config.registry.ownerId`, `systemType` and `region` |
| `exec format error` | The demo images are built for amd64 only; schedule the pods on amd64 nodes, or deploy a release image |
| `ImagePullBackOff` with `not found` | The tag is not in `image.repository`: releases are in `images/cmk-krypton-bridge`, demo images in `cmk-krypton-bridge/images/cmk-krypton-bridge` |
| `helm` stops with `… were replaced in chart 0.2.0` | Values of chart 0.1.0; see [Upgrading from 0.1.0](#upgrading-from-010) |

## Uninstall

```sh
helm uninstall cmk-krypton-bridge -n services
```

The Secrets and the database are not deleted.
