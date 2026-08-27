# syntax=docker/dockerfile:1
# Multi-arch: linux/amd64 (x86_64) and linux/arm64 (aarch64).
# Frontend is built on BUILDPLATFORM; Go is cross-compiled for TARGETOS/TARGETARCH.

ARG GO_VERSION=1.25
ARG NODE_VERSION=22

FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/ipv6-proxy ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates iproute2 tzdata \
    && mkdir -p /etc/ipv6-proxy
COPY --from=build /out/ipv6-proxy /usr/local/bin/ipv6-proxy
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
# Runtime needs NET_ADMIN to add `ip -6 route add local <prefix>`.
# Use host network so FREEBIND can use the host HE /64.
EXPOSE 8080 8081 8082
ENTRYPOINT ["/entrypoint.sh"]
