# Repository Guidelines

## Project Structure & Module Organization

`cmd/tcp-over-kafka/` contains the Cobra CLI entrypoint. Core packages live in
`pkg/`: `frame` for the Kafka wire format, `socks5` for proxy negotiation,
`sshproxy` for OpenSSH `ProxyCommand` bridging, and `tunnel` for client/server
runtime logic and integration tests. Deployment assets live in
`deploy/systemd/`, helper scripts and environment configuration live in
`hack/`, and validation runbooks live in `docs/`.

## Build, Test, and Development Commands

- `make fmt`: run `gofmt` across the Go source tree.
- `make vet`: run `go vet` in vendor mode.
- `make test`: run the full Go test suite with vendored dependencies.
- `make build`: build `./bin/tcp-over-kafka`.
- `./bin/tcp-over-kafka -v`: print the injected build version.
- `make deploy-all`: deploy broker, client, and server with the default
  `systemd` mode.
- `make deploy-docker` / `make deploy-kubernetes`: deploy the same services
  with the alternate supported modes.

## Coding Style & Naming Conventions

Use idiomatic Go and keep all files `gofmt`-compatible. Package names stay
short and lower-case, matching directory names such as `frame` and `tunnel`.
Prefer descriptive CLI flags like `--platform-id`, `--route`, and `--service`.
When the Kafka protocol changes, add a new frame version instead of overloading
existing fields.

## Testing Guidelines

Write tests as `*_test.go` files beside the package they cover. Prefer
table-driven unit tests for parsing, validation, and codec logic, and keep
integration coverage focused in `pkg/tunnel/`. Run `make test` before opening a
PR. For env-backed smoke checks, use the runbooks in `docs/` from the repo root.

## Commit & Pull Request Guidelines

Recent history uses short, imperative subjects such as `Add regression coverage and docs`.
Keep commits focused and use a single-commit PR unless instructed otherwise.
Include a `Signed-off-by:` trailer in every commit message. In PRs, summarize
behavioral impact, list validation commands run, and call out deployment or
operator changes when `hack/`, `deploy/systemd/`, or `README.md` changes.

## Configuration & Operations

Treat `hack/.env.local` as the repo-local source of truth for deployment and
validation settings. Keep path references in docs relative to the repository
when possible, and do not spread environment-specific values across scripts,
units, and runbooks.
