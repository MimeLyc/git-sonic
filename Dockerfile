# Stage 1: Build Go binary
FROM golang:1.21-bookworm AS builder

WORKDIR /src
COPY go.mod go.su[m] ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /git-sonic ./cmd/git-sonic

# Stage 2: Runtime environment
FROM node:22-bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends git bash curl ca-certificates && \
    rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code

COPY --from=builder /git-sonic /usr/local/bin/git-sonic

RUN useradd -m -s /bin/bash appuser && \
    mkdir -p /data/workdir && \
    chown appuser:appuser /data/workdir

USER appuser
WORKDIR /data/workdir

EXPOSE 8080

ENTRYPOINT ["git-sonic"]
