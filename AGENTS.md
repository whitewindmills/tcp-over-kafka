# Repository Guidelines

## Project Structure & Module Organization

- `cmd/tcp-over-kafka/` holds the main binary entrypoint and subcommands.
- `internal/frame/` contains the wire-format codec used for Kafka messages.
- `internal/tunnel/` contains the client/server relay logic and Kafka transport.
- `deploy/systemd/` stores the unit files used on the VMs.
- `README.md` documents the deployed topology and operator commands.
- `session-note.md` is the recovery checkpoint for long sessions.

## Build, Test, and Development Commands

- `CGO_ENABLED=0 go test ./...` runs the full Go test suite.
- `CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o tcp-over-kafka ./cmd/tcp-over-kafka` builds the binary.
- `systemctl enable --now tcp-over-kafka-client` starts the client service on `10.253.15.167`.
- `systemctl enable --now tcp-over-kafka-server` starts the server service on `10.253.15.168`.

## Coding Style & Naming Conventions

- Use `gofmt` for all Go files; tabs are the default indentation.
- Keep package names short and lower-case, matching existing paths such as `frame` and `tunnel`.
- Prefer descriptive flag names like `--tunnel-id`, `--listen`, and `--target`.
- Keep the binary protocol explicit and versioned; add new frame kinds rather than overloading existing ones.

## Testing Guidelines

- Add table-driven tests for protocol and codec changes under the affected package, for example `internal/frame/frame_test.go`.
- Prefer small unit tests over integration-heavy checks.
- For deployment validation, verify broker reachability, direct SSH refusal between peers, and a tunneled SSH hostname check from `167`.

## Commit & Pull Request Guidelines

- Use concise, imperative commit subjects, for example `Add frame round-trip tests`.
- Include `Signed-off-by` in commit messages.
- Keep pull requests focused on one change set and summarize the runtime impact.
- Mention any deployment or operator steps that changed, especially if systemd units or VM setup are affected.

## Operational Notes

- Existing `mqtt-forward` services must be stopped before Kafka tunnel validation, but not removed.
- The current POC uses Redpanda on `10.253.15.166:9092` and SSH tunneling between `10.253.15.167` and `10.253.15.168`.
