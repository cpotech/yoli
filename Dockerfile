# Build stage: compile a single static yoli binary.
FROM golang:1.23 AS build
WORKDIR /src

# Version string injected at build time. Defaults to the result of
# `git describe --tags --dirty --always` (or the short SHA / "dev" fallback)
# so the binary reports the same version as the host `scripts/build.sh`.
# Pass a value via `docker build --build-arg YOLI_VERSION=...`.
ARG YOLI_VERSION=""
RUN set -euo pipefail; \
    if [ -z "$YOLI_VERSION" ] && command -v git >/dev/null 2>&1; then \
      YOLI_VERSION="$(git describe --tags --dirty --always 2>/dev/null || true)"; \
    fi; \
    if [ -z "$YOLI_VERSION" ]; then YOLI_VERSION="dev"; fi; \
    echo "Dockerfile build version=$YOLI_VERSION" >&2
ENV YOLI_VERSION="$YOLI_VERSION"

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0 produces a self-contained static binary. The version is
# stamped into yoli/internal/cli.Version via -ldflags.
RUN CGO_ENABLED=0 go build \
      -ldflags="-s -w -X yoli/internal/cli.Version=${YOLI_VERSION}" \
      -o /out/yoli ./cmd/yoli

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
