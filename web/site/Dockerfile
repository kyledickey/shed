# The public site. Build with the repository root as the context, which the
# footer's version needs for CHANGELOG.md:
#   docker build -f web/site/Dockerfile .
FROM oven/bun:1 AS build
WORKDIR /web
COPY web/package.json web/bun.lock ./
RUN bun ci
COPY CHANGELOG.md /
COPY web .
RUN bun run site:build

FROM caddy:2-alpine
COPY web/site/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /web/site-dist/client /srv
