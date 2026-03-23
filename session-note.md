### Session checkpoint

#### Current goal
Keep the tcp-over-kafka POC recoverable on disk so the session can be resumed without chat history.

#### Task boundary
This checkpoint is only for handoff and recovery. It is not changing the implementation or deployment state.

#### Confirmed facts
- `tcp-over-kafka` is implemented in Go and builds successfully with `CGO_ENABLED=0 go test ./...`.
- The tunnel now uses a SOCKS5 client front end and forwards requested destinations through Kafka.
- The frame protocol includes a `targetAddr` in `KindOpen` and a `KindReady` control frame before client data flows.
- `mqtt-forward` processes on `10.253.15.167` and `10.253.15.168` were stopped, not removed.
- Redpanda is installed and running on `10.253.15.166:9092`.
- Tunnel services are installed and running on `10.253.15.167` and `10.253.15.168`.
- Direct `167 -> 168:22` remains refused.
- Local tests still pass with `CGO_ENABLED=0 go test ./...`.
- A design-focused `README.md` documents the SOCKS5 proxy, Kafka framing, diagrams, and relay risks with generic placeholders.
- A contributor guide was added at `AGENTS.md`.
- The deployment units now reflect the SOCKS5 model and no longer hard-code a fixed relay target in the server service.

#### Relevant files
- [`/opt/codes/poc/tcp-over-kafka/cmd/tcp-over-kafka/main.go`](/opt/codes/poc/tcp-over-kafka/cmd/tcp-over-kafka/main.go)
- [`/opt/codes/poc/tcp-over-kafka/internal/frame/frame.go`](/opt/codes/poc/tcp-over-kafka/internal/frame/frame.go)
- [`/opt/codes/poc/tcp-over-kafka/internal/tunnel/bus.go`](/opt/codes/poc/tcp-over-kafka/internal/tunnel/bus.go)
- [`/opt/codes/poc/tcp-over-kafka/internal/tunnel/client.go`](/opt/codes/poc/tcp-over-kafka/internal/tunnel/client.go)
- [`/opt/codes/poc/tcp-over-kafka/internal/tunnel/server.go`](/opt/codes/poc/tcp-over-kafka/internal/tunnel/server.go)
- [`/opt/codes/poc/tcp-over-kafka/deploy/systemd/tcp-over-kafka-client.service`](/opt/codes/poc/tcp-over-kafka/deploy/systemd/tcp-over-kafka-client.service)
- [`/opt/codes/poc/tcp-over-kafka/deploy/systemd/tcp-over-kafka-server.service`](/opt/codes/poc/tcp-over-kafka/deploy/systemd/tcp-over-kafka-server.service)
- [`/opt/codes/poc/tcp-over-kafka/README.md`](/opt/codes/poc/tcp-over-kafka/README.md)
- [`/opt/codes/poc/tcp-over-kafka/AGENTS.md`](/opt/codes/poc/tcp-over-kafka/AGENTS.md)

#### Key commands run
- `gofmt -w ...`
- `CGO_ENABLED=0 go test ./...`
- `CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o tcp-over-kafka ./cmd/tcp-over-kafka`
- Updated `README.md` to describe the SOCKS5 proxy architecture and Kafka relay risks
- Updated systemd units to match the SOCKS5 proxy model
- Added `internal/socks5` handshake tests

#### Current conclusions
- The POC now forwards TCP through a SOCKS5 client front end over Kafka.
- The most important recovery artifact is this checkpoint plus the deployed service units.
- The checkpoint is still valid after a fresh local test run.
- If the session is resumed, re-check `session-note.md` first, then inspect the tunnel and service files above.

#### Next steps
1. Keep the checkpoint current if more changes are made.
2. If anything is edited, update this file again before ending the session.
3. If a new session starts, read this file first.

#### Handoff note
In a new session, first read:
- `session-note.md`
- the files listed above
- `/opt/codes/AGENTS.md`
