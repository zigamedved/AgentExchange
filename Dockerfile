# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Download dependencies first (cached layer)
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# modernc.org/sqlite is pure Go — CGO_ENABLED=0 is safe.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o bin/ax-platform ./cmd/platform
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o bin/ax          ./cmd/ax
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o bin/ax-mcp      ./cmd/mcp

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
