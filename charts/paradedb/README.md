# paradedb

Standalone ParadeDB chart. Runs the official
[`paradedb/paradedb`](https://hub.docker.com/r/paradedb/paradedb) Docker image
(Postgres 18 with `pg_search` and other extensions preinstalled) as a single
StatefulSet. Outputs a Secret containing **only credentials**
(`username` / `password`) — connection target (host/port/database) is
non-secret and lives in the consumer chart's values.

No CloudNativePG operator. No HA. No backups. Single PVC. Sufficient for
single-instance workloads; harden for production on your own.

## Quick start

```bash
# 1. Deploy paradedb (creates StatefulSet + Service + Secret named paradedb-pg)
helm install paradedb ./charts/paradedb/ -n <namespace>

# 2. Wait for the pod to become ready
kubectl -n <namespace> rollout status sts/paradedb

# 3. Point a consumer chart at it. Example — passion-index:
#    In passion-index's values file:
#      database:
#        host: paradedb         # = paradedb's Service name
#        port: 5432
#        database: postgres     # must match paradedb's database.name
#      secrets:
#        database:
#          existingSecret: "paradedb-pg"
#          usernameKey: username
#          passwordKey: password
helm install passion-index ./charts/passion-index/ -n <namespace> -f prod-values.yaml
```

## Defaults

When `database.user` and `database.name` are both empty (the defaults), the
chart mirrors the postgres image defaults:

| Field | Default | Why |
|---|---|---|
| `database.user` | empty → `postgres` | matches `${POSTGRES_USER:-postgres}` |
| `database.name` | empty → mirrors resolved user | matches `${POSTGRES_DB:-$POSTGRES_USER}` |

If you want a dedicated database + user, set both explicitly:

```yaml
database:
  name: my_app
  user: my_app
```

## Retrieve the generated password

When `database.password` is empty (the default), the chart generates a random
32-char password on first install and writes it into the Secret under the
`password` key:

```bash
kubectl -n <namespace> get secret paradedb-pg -o jsonpath='{.data.password}' | base64 -d
```

The same value is preserved across `helm upgrade` (the chart uses `lookup` to
keep the live password rather than regenerating). Once you've verified the
deployment, consider pinning `database.password` in your values file so that
offline `helm template` renders become deterministic.

## Consumer-side wiring

The Secret contains only `username` and `password`. Consumers must source
`host`, `port`, and `database` from their own values — these are non-secret
and paradedb's defaults are:

| field | value | source |
|---|---|---|
| host | `<release>-paradedb` (Service name) | paradedb's fullname helper |
| port | `5432` | fixed |
| database | `database.name` (empty → mirrors user → `postgres`) | paradedb's values |

If the consumer overrides `database.user` / `database.name` on this chart,
it must set the matching `database.database` value on its own side.

## Values

| Key | Default | Notes |
| --- | --- | --- |
| `image.repository` | `paradedb/paradedb` | Override to mirror through your private registry (recommended for Aliyun ACK pulls) |
| `image.tag` | `0.25.2` | ParadeDB release; bump together with a planned `helm upgrade` |
| `image.pullPolicy` | `IfNotPresent` | Standard K8s values |
| `database.name` | `""` | Empty → mirrors user (PG image default) |
| `database.user` | `""` | Empty → `postgres` (PG image default) |
| `database.password` | `""` | Empty → auto-generate 32-char random |
| `storage.size` | `40Gi` | PVC request |
| `storage.storageClass` | `""` | Empty = cluster default |
| `resources.requests` | `cpu: 0, memory: 0` | Floor for tests; bump for prod |
| `resources.limits` | `cpu: 8000m, memory: 12Gi` | |
| `secret.name` | `paradedb-pg` | Name of Secret created by this chart |
| `secret.existingSecret` | `""` | Non-empty → skip Secret creation, use an externally managed one |
| `fullnameOverride` / `nameOverride` | `""` | Standard helm overrides |

## Operations

### Upgrade the ParadeDB version

```bash
helm upgrade paradedb ./charts/paradedb/ -n <namespace> \
  --set image.tag=0.26.0
```

The StatefulSet rolls the pod. Data on the PVC is preserved. Always check
ParadeDB's release notes for extension upgrade steps (e.g. `ALTER EXTENSION
pg_search UPDATE`).

### Wipe all data (destructive)

```bash
helm uninstall paradedb -n <namespace>
kubectl -n <namespace> delete pvc data-paradedb-0
```

The PVC is **not** deleted by `helm uninstall` (StatefulSet PVC retention
policy). `database.name` / `database.user` / `database.password` changes only
take effect after the PVC is wiped — `initdb` runs once per empty volume.

### Use your own Secret

If you manage DB credentials out-of-band (sealed-secrets, external-secrets,
manual rollout), skip chart-managed Secret generation:

```yaml
secret:
  existingSecret: my-team-managed-secret
```

The referenced Secret must provide the `username` and `password` keys. The
chart's StatefulSet continues to read `password` from it.

## Not in scope

This chart is intentionally minimal. It does **not** provide:

- High availability (single replica)
- Automated backups (WAL archiving, snapshots)
- Point-in-time recovery
- TLS for client connections
- Cross-namespace replication

For any of those, the official
[`paradedb/paradedb`](https://github.com/paradedb/charts) helm chart (built on
CloudNativePG) is the right tool.
