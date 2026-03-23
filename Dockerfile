FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN go mod download
RUN CGO_ENABLED=0 go test ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tcp-over-kafka ./cmd/tcp-over-kafka

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/tcp-over-kafka /usr/local/bin/tcp-over-kafka
ENTRYPOINT ["/usr/local/bin/tcp-over-kafka"]
