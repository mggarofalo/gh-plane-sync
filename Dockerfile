FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /gh-plane-sync ./cmd/gh-plane-sync

FROM alpine:3.21

LABEL org.opencontainers.image.title="gh-plane-sync" \
      org.opencontainers.image.description="Two-way sync bridge between GitHub Issues and Plane work items" \
      org.opencontainers.image.source="https://github.com/mggarofalo/gh-plane-sync"

RUN apk add --no-cache ca-certificates tzdata

RUN adduser -D -u 1000 syncer

COPY --from=build /gh-plane-sync /usr/local/bin/gh-plane-sync

RUN mkdir -p /etc/gh-plane-sync /var/lib/gh-plane-sync \
    && chown syncer:syncer /var/lib/gh-plane-sync

USER syncer

VOLUME ["/etc/gh-plane-sync", "/var/lib/gh-plane-sync"]

ENTRYPOINT ["gh-plane-sync"]
CMD ["--config", "/etc/gh-plane-sync/config.yaml", "--once"]
