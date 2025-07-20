########################################################
# args
########################################################

ARG BUSYBOX_VERSION=1.36.1
ARG GO_VERSION=1.25rc1
ARG ALPINE_VERSION=3.22
ARG ALPINE_BUSYBOX_VERSION=3.21
ARG DEBIAN_CODENAME=bookworm
ARG SOCAT_VERSION=1.8.0.3
ARG TZDATA_VERSION=2025b

ARG JQ_GIT_REF=master
ARG LSHW_GIT_REF=master
ARG E2FSPROGS_GIT_REF=master
ARG RUNC_GIT_REF=main

FROM alpine:${ALPINE_VERSION} AS alpine-ref
FROM alpine:${ALPINE_BUSYBOX_VERSION} AS alpine-busybox-ref
FROM debian:${DEBIAN_CODENAME}-slim AS debian-ref
FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS alpine-golang-ref
FROM golang:${GO_VERSION}-${DEBIAN_CODENAME} AS debian-golang-ref

########################################################
# runm-linux-go-base builder
########################################################

FROM alpine-golang-ref AS runm-linux-go-base-builder

COPY --from=forks / /app/
COPY --from=runm . /app/runm

WORKDIR /app/runm

RUN ls -lash /app

RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod <<EOF
go mod download -x
EOF

########################################################
# runm-linux-mounter builder
########################################################
FROM runm-linux-go-base-builder AS runm-linux-mounter-builder

WORKDIR /app/runm

ARG ARCH 


RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod <<EOF
GOOS=linux GOARCH=${ARCH} go build -x -ldflags="-s -w -extldflags=-static" -trimpath -o /out/runm-linux-mounter ./cmd/runm-linux-mounter
EOF


# note: curr prob
#16 9.925 go: creating work dir: stat /tmp: no such file or directory
