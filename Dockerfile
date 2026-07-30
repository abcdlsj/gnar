FROM rust:alpine3.22 AS builder

RUN apk add --no-cache make musl-dev perl
WORKDIR /src
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN cargo build --locked --release

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S gnar \
    && adduser -S -G gnar -h /data gnar \
    && mkdir -p /data \
    && chown gnar:gnar /data
COPY --from=builder /src/target/release/gnar /usr/local/bin/gnar

WORKDIR /data
VOLUME ["/data"]
EXPOSE 8910
USER gnar
STOPSIGNAL SIGTERM
ENTRYPOINT ["gnar"]
CMD ["serve", "--listen", "0.0.0.0:8910", "--public-url", "http://127.0.0.1:8910", "--database", "/data/gnar.db", "--anonymous-only", "--allow-public-bind"]
