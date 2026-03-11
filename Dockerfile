FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/gostudentubl ./cmd/gostudentubl

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -H -s /sbin/nologin app

WORKDIR /app
RUN chown -R app:app /app
COPY --from=builder /out/gostudentubl /usr/local/bin/gostudentubl

USER app
ENTRYPOINT ["/usr/local/bin/gostudentubl"]
