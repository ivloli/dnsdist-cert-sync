FROM golang:1.21-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/dnsdist-cert-sync .

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        dnsdist \
        pdns-recursor \
        tzdata \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/dnsdist-cert-sync /usr/local/bin/dnsdist-cert-sync
COPY docker/entrypoint.sh /app/entrypoint.sh

RUN chmod +x /app/entrypoint.sh \
    && mkdir -p /etc/dnsdist /etc/dnsdist-cert-sync /etc/powerdns/recursor.d /var/lib/dns-center

EXPOSE 10530/tcp 10531/tcp 19153/tcp 28083/tcp

ENTRYPOINT ["/app/entrypoint.sh"]
