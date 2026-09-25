# The server image: api, worker, migrate and admin in one small image, chosen
# by the command. One image means the migration that runs before a deploy is
# exactly the code the deploy then serves.
#
#   docker build -t coachpulse .
#   docker run coachpulse api | worker | migrate up | admin list-tenants

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
# timetzdata embeds the zone database: the worker runs each practice's jobs at
# that practice's local midnight, and must not depend on the base image
# carrying the zones.
RUN CGO_ENABLED=0 go build -trimpath -tags timetzdata -ldflags="-s -w" \
      -o /out/ ./cmd/api ./cmd/worker ./cmd/migrate ./cmd/admin

# No shell, no package manager, runs as an unprivileged user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /usr/local/bin/
ARG RELEASE=dev
ENV RELEASE=${RELEASE} APP_ENV=production LOG_FORMAT=json
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 CMD ["api", "healthcheck"]
CMD ["api"]
