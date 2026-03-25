FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY go.sum ./
COPY vendor ./vendor
COPY cmd ./cmd
COPY pkg ./pkg
ARG VERSION=dev
RUN CGO_ENABLED=0 go test -mod=vendor ./...
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/tcp-over-kafka ./cmd/tcp-over-kafka

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/tcp-over-kafka /tcp-over-kafka
ENTRYPOINT ["/tcp-over-kafka"]
