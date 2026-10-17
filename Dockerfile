# git-planner as a container: build stage compiles the binary (the
# frontend bundle is committed under internal/assets, so no Node here), the
# runtime stage carries only what the collectors shell out to.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/git-planner .

FROM alpine:3.22
# git: every collector reads the repo through it. graphviz: evidence .dot files
# are rendered with `dot`. bd is optional — its cards fail open without it.
RUN apk add --no-cache ca-certificates git graphviz \
 && adduser -D -u 10001 -h /home/gkp gkp
COPY --from=build /out/git-planner /usr/local/bin/git-planner
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
USER gkp
ENV HOME=/home/gkp
WORKDIR /home/gkp
EXPOSE 8765
# Display zone of the view (letzte-aktionen, #126); the zone data is built
# into the binary, stores and logs stay in UTC.
ENV TZ=Europe/Berlin GKP_ZONE=Europe/Berlin
# The loopback listener is always on, whatever --host says.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO /dev/null http://127.0.0.1:8765/ || exit 1
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
