# tcp-over-kafka Concurrency Test Runbook

## Purpose

Use this document to validate that the shared Kafka topic can carry multiple
concurrent sessions across the symmetric node architecture without cross-stream
contamination.

## Inputs

Run these commands from the repository root. Source the environment file first:

```bash
set -a
source ./hack/.env.local
set +a
```

The primary validation scripts are:

- `./hack/test/ssh.sh`
- `./hack/test/https.sh`

## What To Run

1. Deploy the broker and both nodes with the same `KAFKA_TOPIC`.

```bash
make deploy-all
```

2. Run the baseline SSH and HTTPS scripts once.

```bash
make e2e-test-ssh
make e2e-test-https
```

3. Increase the concurrent session count in stages from the control host by
   running the two scripts in parallel:

- `1 SSH + 1 HTTPS`
- `5 SSH + 5 HTTPS`
- `8 SSH + 8 HTTPS`
- `10 SSH + 10 HTTPS`

Example:

```bash
bash ./hack/test/ssh.sh &
bash ./hack/test/https.sh &
wait
```

## Expected Results

- Lower concurrency levels should complete without timeouts.
- Each SSH or HTTPS request should remain isolated to its own connection ID.
- Traffic should not be observed on the wrong destination `eid`.
- The first failure, if any, should indicate broker or target pressure rather
  than frame corruption.

## Execution History

### 2026-03-26

| Sweep | SSH | HTTPS | Notes |
| --- | --- | --- | --- |
| `1 SSH + 1 HTTPS` | `1/1` | `1/1` | pass (`36s`) |
| `5 SSH + 5 HTTPS` | `5/5` | `5/5` | pass (`22s`) |
| `8 SSH + 8 HTTPS` | `5/8` | `5/8` | first mixed-load failure (`39s`) |
| `10 SSH + 10 HTTPS` | `7/10` | `4/10` | heavier degradation under load (`33s`) |

- Environment source: `./hack/.env.local`
- First failing sweep: `8 SSH + 8 HTTPS`
- Notes: the first captured failure was `ssh: connect to host 10.253.15.167 port 22: Connection refused` during the `8 SSH + 8 HTTPS` sweep. Additional failures at `8+8` and `10+10` often aborted during the per-run `assert_node_ready node-a` checks before the SSH or HTTPS probe completed, which points to node-a SSH access becoming the limiting factor under this concurrency level.

## Troubleshooting

- If only HTTPS fails, verify that the local `services.443` mapping still points
  to `127.0.0.1:443` on both nodes.
- If SSH and HTTPS interfere with each other, inspect the source and
  destination `nid/eid` values written into the open frames.
- If both directions fail at once, check broker health and verify that both
  nodes are still subscribed to the shared topic.
