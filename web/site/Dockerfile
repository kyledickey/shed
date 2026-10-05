# The public site. Build with web/ as the context:
#   docker build -f site/Dockerfile web
FROM oven/bun:1 AS build
WORKDIR /web
COPY package.json bun.lock ./
RUN bun ci
COPY . .
RUN bun run site:build

FROM caddy:2-alpine
COPY site/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /web/site-dist/client /srv
