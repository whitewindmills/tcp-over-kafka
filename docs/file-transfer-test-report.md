# tcp-over-kafka File Transfer Test Runbook

## Purpose

Use this document to verify that the symmetric node tunnel preserves file
contents for SSH `scp` transfers and larger payloads in both directions.

## Inputs

Run these commands from the repository root. Source the environment file first:

```bash
set -a
source ./hack/.env.local
set +a
```

The file-transfer entrypoint is:

- `./hack/test/file-transfer.sh`

The payload sizes come from `E2E_FILE_SIZES_MB` in `./hack/.env.local`.

## What To Run

1. Deploy the broker and both nodes.

```bash
make deploy-all
```

2. Run the directional file-transfer suite.

```bash
make e2e-test-file
```

3. If you need a deeper sweep, override the payload sizes for that run.

```bash
E2E_FILE_SIZES_MB=1,16,64,256 bash ./hack/test/file-transfer.sh
```

## Expected Results

- The upload checksum should match the checksum reported on the destination host
  for every configured size.
- Transfers should succeed for both `node-a -> node-b` and `node-b -> node-a`.
- Larger transfers should complete without truncation or corruption.
- File contents should remain intact even when the proxy is carrying other
  traffic on the same topic.

## Execution History

### 2026-03-26

| Direction | Payload sizes | Result | Notes |
| --- | --- | --- | --- |
| `node-a -> node-b` | `1 MiB, 64 MiB` | pass | Executed through `bash ./hack/test/run-all.sh` |
| `node-b -> node-a` | `1 MiB, 64 MiB` | pass | Executed through `bash ./hack/test/run-all.sh` |

- Environment source: `./hack/.env.local`
- First failing size: none
- Notes: `E2E_FILE_SIZES_MB` was `1,64` for this run and both directional checksum checks succeeded.

## Troubleshooting

- If the transfer fails before any bytes move, confirm that the source node’s
  `routes` config includes the destination SSH endpoint.
- If the remote checksum differs, inspect tunnel logs for `KindClose` or
  `KindError`.
- If large transfers stall, reduce the payload size and verify that both the
  broker and the destination `ssh` service are healthy.
