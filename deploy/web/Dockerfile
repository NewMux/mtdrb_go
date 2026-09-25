# The web image: the app's web build, served by Caddy, which also terminates
# TLS for the whole deployment and proxies the API on the same origin. Same
# origin is not a convenience: the web build keeps its refresh token in a
# SameSite=Strict cookie scoped to /v1, which a browser only sends to the
# origin that set it.
#
# The API URL is compiled into the app, so the image is built per domain:
#   docker build -f deploy/web/Dockerfile --build-arg DOMAIN=app.example.com .

FROM node:22-alpine AS build
WORKDIR /app
COPY app/package.json app/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY app/ ./
ARG DOMAIN
RUN test -n "$DOMAIN" || (echo "build with --build-arg DOMAIN=your.domain" && exit 1)
ENV EXPO_PUBLIC_API_URL=https://${DOMAIN} EXPO_NO_TELEMETRY=1
RUN npx expo export --platform web --output-dir /web

FROM caddy:2-alpine
COPY --from=build /web /srv
COPY deploy/web/Caddyfile /etc/caddy/Caddyfile
