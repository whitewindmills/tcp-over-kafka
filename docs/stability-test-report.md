# tcp-over-kafka Stability Test Runbook

## Purpose

Use this document to validate the single-topic routing model, the frame codec, and the basic tunnel lifecycle after code changes.

## Inputs

Run these commands from the repository root. Load the environment file first:

```bash
set -a
source ./hack/.env.local
set +a
```

The env-backed Kafka smoke test is `TestKafkaSingleTopicRoundTripFromLocalEnv`.

Build repeated flags from the JSON maps:

```bash
CLIENT_ROUTE_FLAGS=$(jq -r 'to_entries[] | "--route \(.key)=\(.value)"' <<<"$CLIENT_ROUTES_JSON")
SERVER_SERVICE_FLAGS=$(jq -r 'to_entries[] | "--service \(.key)=\(.value)"' <<<"$SERVER_SERVICES_JSON")
SSH_DESTINATION="${SSH_TEST_USER}@${SSH_TEST_HOST}"
PROXY_COMMAND="./bin/tcp-over-kafka proxy --socks ${SOCKS_PROXY_ADDR} %h %p"
```

## What To Run

1. Run the full Go suite.

```bash
make test
```

2. Start the client and server with the shared topic and env-backed flags.

```bash
./bin/tcp-over-kafka client \
  --topic "$KAFKA_TOPIC" \
  --broker "$BROKER_ADDR" \
  --group "$CLIENT_GROUP" \
  --listen "$CLIENT_LISTEN_ADDR" \
  --platform-id "$CLIENT_PLATFORM_ID" \
  --device-id "$CLIENT_DEVICE_ID" \
  --max-frame "$CLIENT_MAX_FRAME_SIZE" \
  $CLIENT_ROUTE_FLAGS
```

```bash
./bin/tcp-over-kafka server \
  --topic "$KAFKA_TOPIC" \
  --broker "$BROKER_ADDR" \
  --group "$SERVER_GROUP" \
  --platform-id "$SERVER_PLATFORM_ID" \
  --max-frame "$SERVER_MAX_FRAME_SIZE" \
  $SERVER_SERVICE_FLAGS
```

3. Run the Kafka smoke test.

```bash
CGO_ENABLED=0 go test -mod=vendor ./pkg/tunnel -run TestKafkaSingleTopicRoundTripFromLocalEnv -count=1
```

4. Run the SSH and HTTPS smoke checks through the local SOCKS proxy address from the env file.

```bash
ssh -o ProxyCommand="${PROXY_COMMAND}" \
  "$SSH_DESTINATION" hostname
curl --socks5-hostname "$SOCKS_PROXY_ADDR" "$HTTPS_TEST_URL"
```

5. If `EXPECT_DIRECT_SSH_REFUSED=true`, verify that direct SSH is still refused outside the tunnel from the client host.

## Expected Results

- The local test suite should pass.
- The client should accept SOCKS5 requests on `CLIENT_LISTEN_ADDR`.
- The server should consume the shared topic and only handle frames addressed to `SERVER_PLATFORM_ID`.
- A valid route should produce `KindOpen`, `KindOpenAck`, `KindReady`, `KindData`, and `KindClose` traffic on the same topic.
- A missing client route should fail before any Kafka publish.
- A missing server service should return `KindError`.
- The tunneled SSH and HTTPS checks should succeed through `SOCKS_PROXY_ADDR`.

## Execution History

### 2026-03-25

- Environment source: `./hack/.env.local`
- Broker reachability: pass
- Client service status after redeploy: pass
- Server service status after redeploy: pass
- SSH hostname check through the tunnel: pass
- HTTPS check through the tunnel: pass
- Direct SSH refusal outside the tunnel: pass
- Notes: `make test` passed, the env-backed Kafka smoke test passed, and the runtime now discards unreadable frames and starts new readers at `LastOffset`

## Troubleshooting

- If the broker is unreachable, verify `BROKER_ADDR` in `./hack/.env.local`.
- If the client accepts SOCKS5 but no session opens, check `CLIENT_ROUTES_JSON`.
- If the server never dials a target, check `SERVER_SERVICES_JSON`.
- If tests hang after a broker restart, restart the broker, client, and server services before rerunning.
