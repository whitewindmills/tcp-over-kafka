# tcp-over-kafka File Transfer Test Runbook

## Purpose

Use this document to verify that the shared-topic tunnel preserves file contents for SSH `scp` transfers and larger payloads.

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
PAYLOAD_PATH=./payload.bin
```

## What To Run

1. Start the tunnel endpoints with the shared topic.

2. Generate a payload.

```bash
dd if=/dev/urandom of="$PAYLOAD_PATH" bs=1M count=1
```

3. Copy the payload through the local SOCKS proxy path.

```bash
scp -o ProxyCommand="${PROXY_COMMAND}" \
  "$PAYLOAD_PATH" "${SSH_DESTINATION}:${SCP_REMOTE_PATH}"
```

4. Compare checksums on both sides.

```bash
sha256sum "$PAYLOAD_PATH"
ssh -o ProxyCommand="${PROXY_COMMAND}" \
  "$SSH_DESTINATION" sha256sum "$SCP_REMOTE_PATH"
```

5. Sweep larger payload sizes:

- `1 MiB`
- `4 MiB`
- `16 MiB`
- `64 MiB`
- `256 MiB`
- `512 MiB`

## Expected Results

- The upload checksum should match the checksum reported on the destination host.
- Larger transfers should complete without truncation or corruption.
- File contents should remain intact even when the proxy is carrying other traffic on the same topic.

## Execution History

### 2026-03-25

| Payload size | Local checksum | Remote checksum | Duration | Notes |
| --- | --- | --- | --- | --- |
| `1 MiB` | `163fcaf98ae843c8ed46685be68ab064e10cf89ba58e4913ce8870bd0bc64167` | `163fcaf98ae843c8ed46685be68ab064e10cf89ba58e4913ce8870bd0bc64167` | `2s` | pass |
| `4 MiB` | `68e80157f1e33d160e6a03c8dbcf4638a194243b782ecfb3ac9a64e45e739aca` | `68e80157f1e33d160e6a03c8dbcf4638a194243b782ecfb3ac9a64e45e739aca` | `3s` | pass |
| `16 MiB` | `30bd1e4e0ef721775248e9d4078884dfcea24889a2d84778c3d92b54a4a4dc67` | `30bd1e4e0ef721775248e9d4078884dfcea24889a2d84778c3d92b54a4a4dc67` | `4s` | pass |
| `64 MiB` | `fc84e6edad25c9e4c5535a6cd137025349ce1ce9b74156fb1fda84199c36908c` | `fc84e6edad25c9e4c5535a6cd137025349ce1ce9b74156fb1fda84199c36908c` | `20s` | pass |
| `256 MiB` | `809ac89c9648bc6f55a03763c11d9839fc9573a93f3900987697b978968b73a7` | `809ac89c9648bc6f55a03763c11d9839fc9573a93f3900987697b978968b73a7` | `70s` | pass |
| `512 MiB` | `fabca6cb9dc63ea386da66535185968dce2bc66fac4ea4dc404bc30a60a849da` | `fabca6cb9dc63ea386da66535185968dce2bc66fac4ea4dc404bc30a60a849da` | `119s` | pass |

- Environment source: `./hack/.env.local`
- First failing size: none through `512 MiB`
- Notes: checksum integrity remained stable through the full sweep

## Troubleshooting

- If the transfer fails before any bytes move, confirm that `CLIENT_ROUTES_JSON` includes the SSH target.
- If the remote checksum differs, inspect tunnel logs for `KindClose` or `KindError`.
- If large transfers stall, reduce the payload size and verify that both the broker and the remote target service are healthy.
