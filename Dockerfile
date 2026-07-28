# The SQLite driver is modernc.org/sqlite, which is pure Go — so this builds
# with CGO off and the runtime image needs nothing but certificates.
FROM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies first: they change far less often than the source, so this layer
# survives most rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/git-planner .

FROM alpine:3.21

# ca-certificates for api.github.com, tzdata because every timestamp in the UI
# is formatted in local time and an image without it silently renders UTC.
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Europe/Berlin

WORKDIR /app
COPY --from=build /out/git-planner /usr/local/bin/git-planner

# config.yaml and .env are mounted, not baked in: the image must not carry a
# token, and the config changes more often than the code.
EXPOSE 8092
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
  CMD wget -qO- http://127.0.0.1:8092/healthz >/dev/null || exit 1

ENTRYPOINT ["git-planner"]
CMD ["-config", "/app/config.yaml", "-env", "/app/.env"]
