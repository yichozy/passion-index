# syntax=docker/dockerfile:1.7

FROM golang:1.26.1 AS builder

WORKDIR /build

ARG GITHUB_TOKEN

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN go env -w GOPRIVATE=github.com/yichozy
RUN if [ -n "$GITHUB_TOKEN" ]; then \
      git config --global url."https://${GITHUB_TOKEN}@github.com".insteadOf "https://github.com"; \
    fi

COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/passion-index .

FROM debian:bookworm-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 1001 --create-home --shell /usr/sbin/nologin passion

COPY --from=builder /out/passion-index /usr/local/bin/passion-index

USER passion

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/passion-index"]
