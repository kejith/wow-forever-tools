FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/ahsync ./cmd/ahsync

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* \
    && useradd -r -u 10001 -s /usr/sbin/nologin ahsync \
    && mkdir -p /var/lib/wow-ah-sync/blobs && chown -R ahsync:ahsync /var/lib/wow-ah-sync
COPY --from=build /out/ahsync /usr/local/bin/ahsync
USER ahsync
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/ahsync"]
