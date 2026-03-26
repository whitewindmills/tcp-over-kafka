# tcp-over-kafka Stability Test Runbook

## Purpose

Use this document to validate the symmetric node architecture, the single-topic
frame flow, and the baseline SSH/HTTPS tunnel behavior after code changes.

## Inputs

Run these commands from the repository root. Load the environment file first:

```bash
set -a
source ./hack/.env.local
set +a
```

The env-backed Kafka smoke test is `TestKafkaSingleTopicRoundTripFromLocalEnv`.

The primary shell entrypoints are:

- `./hack/test/ssh.sh`
- `./hack/test/https.sh`
- `./hack/test/run-all.sh`

## What To Run

1. Run the full Go suite.

```bash
make test
```

2. Build the binary.

```bash
make build
./bin/tcp-over-kafka -v
```

3. Deploy the broker and both symmetric nodes.

```bash
make deploy-all
```

4. Run the Kafka smoke test.

```bash
CGO_ENABLED=0 go test -mod=vendor ./pkg/tunnel -run TestKafkaSingleTopicRoundTripFromLocalEnv -count=1
```

5. Run the SSH and HTTPS checks in both directions.

```bash
make e2e-test-ssh
make e2e-test-https
```

## Expected Results

- The local test suite should pass.
- Node A and node B should both accept SOCKS5 requests on their configured
  `NODE_*_SOCKS_ADDR` values.
- Each node should consume the shared topic through its own derived consumer
  group and only process frames addressed to its own `platformID`.
- A valid route should produce `KindOpen`, `KindOpenAck`, `KindReady`,
  `KindData`, and `KindClose` traffic on the same topic.
- A missing route should fail before any Kafka publish.
- A missing service should return `KindError` with
  `unknown destination: "<device>"`.
- The tunneled SSH and HTTPS checks should succeed in both directions.

## Execution History

### 2026-03-26

- Environment source: `./hack/.env.local`
- Broker reachability: pass
- Node A service status after deploy: pass (`tcp-over-kafka-node` active, `hello-world-https` active)
- Node B service status after deploy: pass (`tcp-over-kafka-node` active, `hello-world-https` active)
- Kafka smoke test from local env: pass (`CGO_ENABLED=0 go test -mod=vendor ./pkg/tunnel -run TestKafkaSingleTopicRoundTripFromLocalEnv -count=1`)
- SSH checks through the tunnel: pass (`bash ./hack/test/ssh.sh`)
- HTTPS checks through the tunnel: pass (`bash ./hack/test/https.sh`)
- Notes: the first SSH E2E failure came from stale legacy `tcp-over-kafka-client` and `tcp-over-kafka-server` services still running on the remote hosts; after retiring those units, the node-based deployment and checks passed.

## Troubleshooting

- If the broker is unreachable, verify `BROKER_ADDR` in `./hack/.env.local`.
- If SSH fails in only one direction, inspect the `NODE_A_ROUTES_JSON` or
  `NODE_B_ROUTES_JSON` entry for the target host and port.
- If HTTPS fails on one node, inspect the local `services.web` mapping and the
  `hello-world-https` service on that host.
- If tests hang after a broker restart, restart the broker and both node
  services before rerunning.
