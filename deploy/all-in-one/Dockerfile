# syntax=docker/dockerfile:1.7

FROM golang:1.24-alpine AS backend-build

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

WORKDIR /src/backend

RUN apk add --no-cache git ca-certificates tzdata

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend ./

ARG VERSION=dev
ARG BUILD_TIME=unknown

RUN mkdir -p /out/logs /out/storage && \
    touch /out/logs/.keep /out/storage/.keep

RUN go build -trimpath -ldflags="-s -w \
        -X github.com/kleinai/backend/pkg/version.Build=${VERSION} \
        -X github.com/kleinai/backend/pkg/version.Time=${BUILD_TIME}" \
    -o /out/api ./cmd/api && \
    go build -trimpath -ldflags="-s -w \
        -X github.com/kleinai/backend/pkg/version.Build=${VERSION}" \
    -o /out/admin ./cmd/admin && \
    go build -trimpath -ldflags="-s -w \
        -X github.com/kleinai/backend/pkg/version.Build=${VERSION}" \
    -o /out/openai ./cmd/openai && \
    go build -trimpath -ldflags="-s -w \
        -X github.com/kleinai/backend/pkg/version.Build=${VERSION}" \
    -o /out/worker ./cmd/worker

FROM node:20-alpine AS frontend-build

WORKDIR /repo

RUN corepack enable

COPY frontend/pnpm-workspace.yaml frontend/pnpm-lock.yaml* frontend/package.json ./
COPY frontend/tsconfig.base.json ./
COPY frontend/packages packages
COPY frontend/apps/user apps/user
COPY frontend/apps/admin apps/admin

RUN pnpm install --frozen-lockfile=false
RUN pnpm --filter @kleinai/user build
RUN VITE_BASE_PATH=/admin/ pnpm --filter @kleinai/admin build

FROM nginx:1.27-alpine AS runtime

WORKDIR /app

ARG MIHOMO_VERSION=v1.19.10

RUN apk add --no-cache ca-certificates tzdata && \
    wget -q -O /tmp/mihomo.gz "https://github.com/MetaCubeX/mihomo/releases/download/${MIHOMO_VERSION}/mihomo-linux-amd64-compatible-${MIHOMO_VERSION}.gz" && \
    gunzip -c /tmp/mihomo.gz > /usr/local/bin/mihomo && \
    chmod +x /usr/local/bin/mihomo && \
    rm -f /tmp/mihomo.gz

COPY --from=backend-build /out/api /app/api
COPY --from=backend-build /out/admin /app/admin
COPY --from=backend-build /out/openai /app/openai
COPY --from=backend-build /out/worker /app/worker
COPY --from=backend-build /src/backend/configs /app/configs
COPY --from=backend-build /out/logs /app/logs
COPY --from=backend-build /out/storage /app/storage

COPY --from=frontend-build /repo/apps/user/dist /usr/share/nginx/html
COPY --from=frontend-build /repo/apps/admin/dist /usr/share/nginx/html/admin

COPY deploy/all-in-one/nginx.conf.template /etc/nginx/templates/default.conf.template
COPY deploy/all-in-one/entrypoint.sh /app/entrypoint.sh

RUN chmod +x /app/entrypoint.sh && \
    chmod -R 755 /app/logs /app/storage

ENV TZ=Asia/Shanghai
ENV KLEIN_API_PORT=17180
ENV KLEIN_ADMIN_PORT=17188
ENV KLEIN_OPENAI_PORT=17200
ENV KLEIN_LOG_DIR=/app/logs
ENV KLEIN_STORAGE_ROOT=/app/storage
ENV KLEIN_MIHOMO_HOME=/app/private/mihomo
ENV KLEIN_MIHOMO_CONTROLLER=http://127.0.0.1:9090
ENV DAPO_GATEWAY_PORT=3040
ENV API_UPSTREAM=http://127.0.0.1:17180
ENV ADMIN_UPSTREAM=http://127.0.0.1:17188
ENV OPENAI_UPSTREAM=http://127.0.0.1:17200

EXPOSE 3040

ENTRYPOINT ["/app/entrypoint.sh"]
