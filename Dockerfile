# Build stage: compile a single static yoli binary.
FROM golang:1.23 AS build
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0 produces a self-contained static binary.
RUN CGO_ENABLED=0 go build -o /out/yoli ./cmd/yoli

# Runtime stage: minimal image with the tools the agent's Bash tool needs
# (a shell, git, and CA certificates for TLS to providers).
FROM debian:stable-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/yoli /usr/local/bin/yoli

# The agent operates against whatever project is mounted at /work.
WORKDIR /work

ENTRYPOINT ["yoli"]
CMD ["tui"]
