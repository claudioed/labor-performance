# syntax=docker/dockerfile:1.7

# --- build stage ---
FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache go.mod/go.sum download separately from source so editing source
# code doesn't bust the module-download layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# BuildKit cache mounts for the module and build caches speed up repeat
# builds in CI without baking the cache into the image layers.
# All three binaries ship in one image: the OLTP service, plus the
# analytics data product's writer and read-only reader (ADR 0007). They are
# separate PROCESSES, selected at run time via the entrypoint — the
# projector is the only writer of the analytical database, and the reports
# reader never writes or migrates.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/labor ./cmd/labor && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/labor-projector ./cmd/labor-projector && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/labor-reports ./cmd/labor-reports

# --- runtime stage ---
FROM alpine:3.24
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates=20260611-r0 && \
    addgroup -g 1000 -S app && adduser -u 1000 -S app -G app
WORKDIR /app
COPY --from=build --chown=app:app /out/labor ./labor
COPY --from=build --chown=app:app /out/labor-projector ./labor-projector
COPY --from=build --chown=app:app /out/labor-reports ./labor-reports
# Both migration sets: /migrations is the OLTP schema (run by ./labor),
# /migrations/analytics is the analytical schema (run by ./labor-projector,
# and by nothing else).
COPY --from=build --chown=app:app /src/migrations ./migrations
USER 1000
EXPOSE 8080
ENTRYPOINT ["./labor"]
