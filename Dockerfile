# Use a smaller base image for the build stage
FROM golang:1.27.4-alpine AS builder

LABEL stage=gobuilder

ARG TARGETARCH
ARG VERSION=unknown
ARG CHANNEL=dev
# UTC RFC 3339 (YYYY-MM-DDTHH:MM:SSZ); empty means the time of the build.
ARG BUILD_TIME=
ENV CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH}

# Combine apk commands into one to reduce layer size
RUN apk update --no-cache && apk add --no-cache tzdata ca-certificates

WORKDIR /build

# Copy go.mod and go.sum first to take advantage of Docker caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application code
COPY . .

# The writable part of the runtime tree, assembled here because the final
# image has no shell. etc/ ships an empty configuration: a locally initialized
# etc/ppanel.yaml holds the JWT secret and database credentials and must never
# be baked into the image (.dockerignore keeps it out of the build context as
# well); the server completes it on the first start. logs/ receives the log
# files (Logger.Path) and cache/ the GeoIP database (GeoIP.Path).
RUN mkdir -p /out/etc /out/logs /out/cache && : > /out/etc/ppanel.yaml

# Build the binary with version and build time. script/ldflags.sh defines the
# injected metadata for every build path (make, this image, the release).
RUN LDFLAGS="$(VERSION="${VERSION}" CHANNEL="${CHANNEL}" BUILD_TIME="${BUILD_TIME}" sh script/ldflags.sh)" && \
    go build -trimpath -ldflags="${LDFLAGS}" -o /out/ppanel main.go

# Final minimal image
FROM scratch

# Copy CA certificates and timezone data
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo/Asia/Shanghai /usr/share/zoneinfo/Asia/Shanghai

ENV TZ=Asia/Shanghai

# The binary stays owned by root; only the directories the server writes
# belong to the unprivileged user it runs as (65532, the conventional
# "nonroot" id). A volume mounted over one of them must be writable by that
# id on the first start, when the configuration is completed:
#   chown 65532:65532 etc/ppanel.yaml
COPY --from=builder /out/ppanel /app/ppanel
COPY --from=builder --chown=65532:65532 /out/etc /app/etc
COPY --from=builder --chown=65532:65532 /out/logs /app/logs
COPY --from=builder --chown=65532:65532 /out/cache /app/cache

# Set working directory: the configuration, logs and cache paths are relative
# to it.
WORKDIR /app

# The server needs no privileges: it listens on 8080 and writes only under
# /app. Publish a low port with a port mapping (-p 443:8080) rather than by
# changing Port, which an unprivileged process cannot bind.
USER 65532:65532

# Expose the port (optional)
EXPOSE 8080

# The server's own health probe: it asks the running server over the local
# listener and exits non-zero when it gets no healthy answer, so the
# orchestrator restarts or stops routing to a container that is up but not
# serving. The start period covers the first start's database migration.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["/app/ppanel", "healthcheck"]

# Specify entry point
ENTRYPOINT ["/app/ppanel"]
CMD ["run", "--config", "etc/ppanel.yaml"]
