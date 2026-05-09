# syntax=docker/dockerfile:1.7
FROM node:20-alpine AS build
WORKDIR /repo

RUN corepack enable

COPY pnpm-workspace.yaml pnpm-lock.yaml* package.json ./
COPY tsconfig.base.json ./
COPY packages packages
COPY apps/user apps/user
COPY apps/admin apps/admin

RUN pnpm install --frozen-lockfile=false
RUN pnpm --filter @kleinai/user build
RUN VITE_BASE_PATH=/admin/ pnpm --filter @kleinai/admin build

FROM nginx:1.27-alpine AS runtime
COPY --from=build /repo/apps/user/dist /usr/share/nginx/html
COPY --from=build /repo/apps/admin/dist /usr/share/nginx/html/admin
COPY apps/gateway/nginx.conf.template /etc/nginx/templates/default.conf.template

ENV API_UPSTREAM=http://dapo-v2-api:17180
ENV ADMIN_UPSTREAM=http://dapo-v2-admin:17188
ENV OPENAI_UPSTREAM=http://dapo-v2-openai:17200

EXPOSE 80
