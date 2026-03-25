# tcp-over-kafka Concurrency Test Runbook

## Purpose

Use this document to validate that the shared Kafka topic can carry multiple concurrent sessions without cross-stream contamination.

## Inputs

Run these commands from the repository root. Source the environment file first:

```bash
set -a
source ./hack/.env.local
set +a
```

Build repeated flags and test targets:

```bash
CLIENT_ROUTE_FLAGS=$(jq -r 'to_entries[] | "--route \(.key)=\(.value)"' <<<"$CLIENT_ROUTES_JSON")
SERVER_SERVICE_FLAGS=$(jq -r 'to_entries[] | "--service \(.key)=\(.value)"' <<<"$SERVER_SERVICES_JSON")
SSH_DESTINATION="${SSH_TEST_USER}@${SSH_TEST_HOST}"
PROXY_COMMAND="./bin/tcp-over-kafka proxy --socks ${SOCKS_PROXY_ADDR} %h %p"
```

## What To Run

1. Start the client and server with the same `KAFKA_TOPIC` but distinct consumer groups.

2. Run a mixed-session sweep through `SOCKS_PROXY_ADDR`.

```bash
ssh -o ProxyCommand="${PROXY_COMMAND}" \
  "$SSH_DESTINATION" hostname
curl --socks5-hostname "$SOCKS_PROXY_ADDR" "$HTTPS_TEST_URL"
```

3. Increase the concurrent session count in stages:

- `1 SSH + 1 HTTPS`
- `5 SSH + 5 HTTPS`
- `6 SSH + 6 HTTPS`
- `8 SSH + 8 HTTPS`
- `10 SSH + 10 HTTPS`

## Expected Results

- Lower concurrency levels should complete without timeouts.
- Each SSH or HTTPS request should remain isolated to its own connection ID.
- Traffic should not be observed on the wrong destination device.
- The first failure, if any, should indicate broker or target pressure rather than frame corruption.

## Execution History

### 2026-03-25

| Sweep | SSH | HTTPS | Notes |
| --- | --- | --- | --- |
| `1 SSH + 1 HTTPS` | `1/1` | `1/1` | pass |
| `5 SSH + 5 HTTPS` | `5/5` | `5/5` | pass |
| `6 SSH + 6 HTTPS` | `6/6` | `6/6` | pass |
| `8 SSH + 8 HTTPS` | `8/8` | `7/8` | first HTTPS failure |
| `10 SSH + 10 HTTPS` | `10/10` | `7/10` | HTTPS degraded further while SSH stayed stable |

- Environment source: `./hack/.env.local`
- First failing sweep: `8 SSH + 8 HTTPS`
- Notes: the first failures were HTTPS-only; no SSH concurrency failures were observed through `10 SSH + 10 HTTPS`

## Troubleshooting

- If only HTTPS fails, verify the service mapping for the HTTPS helper in `SERVER_SERVICES_JSON`.
- If SSH and HTTPS interfere with each other, inspect the source and destination IDs written into the open frames.
- If both sides fail at once, check broker health and verify that the client and server are still subscribed to the shared topic.
