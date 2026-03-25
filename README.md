# tcp-over-kafka

`tcp-over-kafka` is a proof-of-concept TCP tunnel that carries byte streams over
Kafka. The client exposes a local SOCKS5 proxy, the tunnel publishes framed
traffic to one shared Kafka topic, and the server reconstructs the TCP stream on
the destination side.

## What This Repo Contains

- `client`: accepts local SOCKS5 `CONNECT` requests and publishes tunnel frames.
- `server`: consumes frames from the shared topic, resolves the destination
  device, and relays bytes to the target TCP service.
- `proxy`: bridges OpenSSH `ProxyCommand` stdio to the local SOCKS5 listener.
- `broker`: runs the Redpanda broker used by the POC deployment.

The transport model is intentionally simple:

- one Kafka topic carries all sessions
- frames carry source and destination platform/device identities
- the client route map resolves `host:port` requests to a destination identity
- the server service map resolves a destination device ID to a real TCP target

## Quick Start

1. Edit `./hack/.env.local` for your environment.
2. Run the local checks.

```bash
make test
make build
./bin/tcp-over-kafka -v
```

3. Deploy with the default `systemd` flow.

```bash
make deploy-all
```

4. Load the env file and follow the validation runbooks.

```bash
set -a
source ./hack/.env.local
set +a
```

- [Stability Runbook](./docs/stability-test-report.md)
- [Concurrency Runbook](./docs/concurrency-test-report.md)
- [File Transfer Runbook](./docs/file-transfer-test-report.md)

## Prerequisites

- Go 1.23 or newer for local builds and tests
- `jq` for expanding the JSON route and service maps in `./hack/.env.local`
- `ssh` and `scp` for remote deployment
- Docker on the broker host
- `kubectl` only when using the Kubernetes deployment mode
- Access to the hosts, certificates, and broker defined in `./hack/.env.local`

## Repository Layout

- `./cmd/tcp-over-kafka/`: Cobra entrypoint, root flags, and subcommands
- `./pkg/frame/`: versioned Kafka frame codec
- `./pkg/socks5/`: SOCKS5 handshake and proxy support
- `./pkg/sshproxy/`: OpenSSH `ProxyCommand` bridge
- `./pkg/tunnel/`: client/server runtime, routing, services, and tests
- `./deploy/systemd/`: generic systemd units that call the `./hack/` wrappers
- `./hack/`: env file, deployment scripts, helper scripts, and Kubernetes render
  logic
- `./docs/`: validation runbooks and execution history

## Environment Model

`./hack/.env.local` is the repo-local source of truth for:

- SSH connection details for remote deployment
- broker address and broker deployment settings
- client and server identities
- route and service mappings
- deployment mode selection
- validation targets and local SOCKS proxy address
- HTTPS helper certificate paths

Changing environments should only require editing `./hack/.env.local` and
rerunning the relevant `make deploy-*` target.

## Build, Test, And CLI

Use the `Makefile` for the normal local workflow:

```bash
make vendor
make fmt
make vet
make test
make build
```

The release binary is written to `./bin/tcp-over-kafka`. The root command
supports version output directly:

```bash
./bin/tcp-over-kafka -v
./bin/tcp-over-kafka --version
```

Dependency resolution for `make vet`, `make test`, and `make build` runs in
vendor mode. If `go.mod` or `go.sum` changes, refresh the checked-in
dependencies with `make vendor`.

## Deployment Modes

`systemd` is the default deployment mode. The repo also supports `docker` and
`kubernetes`, and each service can override the default independently through
the variables in `./hack/.env.local`.

| Mode | Primary command | What it does |
| --- | --- | --- |
| `systemd` | `make deploy-all` or `make deploy-systemd` | Copies the env file, binary, and helper scripts to the target hosts, installs generic systemd units, and enables the requested services. |
| `docker` | `make deploy-docker` | Uses the remote deployment script to run the broker, client, and server as long-running Docker containers on the target hosts. |
| `kubernetes` | `make deploy-kubernetes` | Renders manifests from `./hack/render-kubernetes.sh` and applies them with `kubectl`. |

Component-specific deploy targets are also available:

- `make deploy-broker`
- `make deploy-client`
- `make deploy-server`

Mixed-mode deployment is supported through per-component overrides. Example:

```bash
BROKER_DEPLOY_MODE=docker \
CLIENT_DEPLOY_MODE=systemd \
SERVER_DEPLOY_MODE=kubernetes \
make deploy-all
```

The current Docker and Kubernetes flows keep the POC deployment model: the
client and server binaries are still staged onto the target nodes, and the
runtime uses host networking and host-mounted artifacts where the scripts expect
them.

## Validation Docs

Validation history lives with the runbooks instead of in the README:

- [Stability Runbook](./docs/stability-test-report.md)
- [Concurrency Runbook](./docs/concurrency-test-report.md)
- [File Transfer Runbook](./docs/file-transfer-test-report.md)

Each runbook includes the commands to run, the expected outcomes, and an
execution history section for recording real runs.

## Implementation Notes

- Frames are binary and versioned. Add a new frame version instead of
  overloading the existing one.
- Kafka ordering matters. The implementation keeps a stable conversation key per
  connection.
- `./pkg/tunnel/` starts live Kafka readers at `LastOffset` and skips unreadable
  frames so stale topic history does not break a new deployment.
- This project is intentionally scoped as a deterministic POC, not a
  production-ready generic proxy.
