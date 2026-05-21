FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/gostudentubl ./cmd/gostudentubl

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata sudo chromium fonts-dejavu && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd -g 1000 app && \
    useradd -r -u 1000 -g 1000 -s /sbin/nologin -m -d /home/app app && \
    echo 'app ALL=(root) NOPASSWD: SETENV: /usr/local/bin/codex' > /etc/sudoers.d/app-codex && \
    chmod 440 /etc/sudoers.d/app-codex

WORKDIR /app
RUN chown -R app:app /app
COPY --from=builder /out/gostudentubl /usr/local/bin/gostudentubl
COPY assets ./assets

USER app
ENTRYPOINT ["/usr/local/bin/gostudentubl"]
