# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
# Cache downloaded modules across builds (invalidated only when go.mod/go.sum change)
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

# modernc.org/sqlite is pure Go — CGO_ENABLED=0 is safe.
# Build cache mount keeps compiled packages between runs so only changed files recompile.
# ax and ax-mcp are CLI tools — not needed inside the container image.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o bin/ax-platform ./cmd/platform

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /build/bin/ ./bin/

# Platform HTTP port
EXPOSE 8080

# Default: in-memory storage (stateless). For persistent storage, use the
# enterprise or marketplace examples with a mounted volume and SQLite:
#   docker run -v $(pwd)/data:/data -e AX_DB=/data/ax.db ...
ENV AX_PLATFORM_ADDR=:8080

ENTRYPOINT ["./bin/ax-platform"]
