# zashboard is built from the reviewed source in this repository.
ARG CORE_IMAGE=metacubex/mihomo:latest
FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS frontend
WORKDIR /web
RUN npm install -g pnpm@11.20.0
COPY web/zashboard/package.json web/zashboard/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY web/zashboard/ ./
RUN pnpm type-check && pnpm build:no-fonts

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go test ./... && \
    CGO_ENABLED=0 GOAMD64=v1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X main.version=${VERSION}" -o /mymihomo ./cmd/mymihomo/

FROM ${CORE_IMAGE} AS core
FROM alpine:3.22
COPY --from=core /mihomo /mihomo
RUN apk add --no-cache ca-certificates
ADD https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.metadb /root/.config/mihomo/geoip.metadb
ADD https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat /root/.config/mihomo/geosite.dat
ADD https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat /root/.config/mihomo/geoip.dat
COPY --from=builder /mymihomo /bin/mymihomo
COPY --from=frontend /web/dist/ /opt/mymihomo/ui/zashboard/
COPY run.sh /bin/run
RUN chmod +x /bin/run && mkdir -p /root/conf
ENV CONF_FILE=/root/conf/config.yaml MIHOMO_HOME=/root/.config/mihomo \
    UI_DIR=/opt/mymihomo/ui/zashboard WEB_CONFIG_PORT=9097 EXTERNAL_PORT=19097 \
    MIXED_PORT=7897 TZ=Asia/Hong_Kong WEB_ENV_FILE=/root/conf/web.env
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 \
    CMD wget -q -T 3 --header="Authorization: Bearer ${EXTERNAL_SECRET}" -O - "http://127.0.0.1:${EXTERNAL_PORT}/version" >/dev/null && \
    wget -q -T 3 -O - "http://127.0.0.1:${WEB_CONFIG_PORT}/healthz" >/dev/null || exit 1
ENTRYPOINT ["/bin/run"]
