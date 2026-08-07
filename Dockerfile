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

# The homelab's mkcert CA, so the monitor widget can check the services on the
# Mac mini at https://<name>.lan.niclasedge.com. Those publish no port — the
# IaC-Stack contract forbids one next to a domain, because Docker's iptables
# rules sit in front of UFW — so the Traefik route is the only way to observe
# them, and without this CA every such check fails on TLS instead of reporting
# health. A permanently red check is worse than none: it teaches people to look
# away.
#
# This is a public certificate, not a secret; the private half never left the
# MacBook. update-ca-certificates APPENDS to the bundle, so api.github.com and
# every other public endpoint keep validating exactly as before — replacing
# /etc/ssl/certs/ca-certificates.crt with a mount would have broken them.
COPY lan-ca.crt /usr/local/share/ca-certificates/lan-ca.crt
RUN update-ca-certificates

WORKDIR /app
COPY --from=build /out/git-planner /usr/local/bin/git-planner

# config.yaml and .env are mounted, not baked in: the image must not carry a
# token, and the config changes more often than the code.
EXPOSE 8092
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
  CMD wget -qO- http://127.0.0.1:8092/healthz >/dev/null || exit 1

ENTRYPOINT ["git-planner"]
CMD ["-config", "/app/config.yaml", "-env", "/app/.env"]
