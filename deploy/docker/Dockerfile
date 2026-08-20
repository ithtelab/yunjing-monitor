FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata su-exec \
    && addgroup -S -g 10001 monitor \
    && adduser -S -D -H -u 10001 -G monitor monitor

ARG SERVER_BINARY=release/vps-server-linux-amd64

WORKDIR /app
COPY ${SERVER_BINARY} /app/vps-server
COPY deploy/docker/entrypoint.sh /entrypoint.sh
RUN chmod 0755 /app/vps-server /entrypoint.sh \
    && mkdir -p /app/data \
    && chown -R monitor:monitor /app

# Start as root so entrypoint can chown the mounted ./data volume, then drop privileges.
USER root
EXPOSE 1314

ENTRYPOINT ["/entrypoint.sh"]
