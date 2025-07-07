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
# debian-based builder
########################################################

FROM debian-ref AS debian-base

ENV DEBIAN_FRONTEND=noninteractive \
	DEBCONF_NONINTERACTIVE_SEEN=true \
	LC_ALL=C.UTF-8 \
	LANG=C.UTF-8

RUN apt-get update \
	&& apt-get install -y \
		build-essential \
		autoconf \
		libtool \
		git \
		bison \
		flex \
		wget \
	&& rm -rf /var/lib/apt/lists/*


########################################################
# timezone builder
########################################################

FROM alpine-ref AS timezone-builder

WORKDIR /work

RUN curl -L -o tzdata.tar.gz https://data.iana.org/time-zones/tzdata${TZDATA_VERSION}.tar.gz

RUN tar -xf tzdata${TZDATA_VERSION}.tar.gz

ENV ZONEINFO=/out/usr/share/zoneinfo
RUN mkdir -pv ${ZONEINFO}/{posix,right}

RUN <<EOF
for tz in etcetera southamerica northamerica europe africa antarctica  \
		  asia australasia backward pacificnew systemv; do
	zic -L /dev/null   -d ${ZONEINFO}       -y "sh yearistype.sh" ${tz}
	zic -L /dev/null   -d ${ZONEINFO}/posix -y "sh yearistype.sh" ${tz}
	zic -L leapseconds -d ${ZONEINFO}/right -y "sh yearistype.sh" ${tz}
done
EOF

RUN cp -v zone.tab zone1970.tab iso3166.tab ${ZONEINFO}
# RUN zic -d ${ZONEINFO} -p America/New_York
########################################################
# busybox builder2
########################################################

FROM alpine-ref AS busybox-static-builder-2
ARG BUSYBOX_VERSION

RUN apk add --no-cache "busybox-static>${BUSYBOX_VERSION}"

# RUN which busybox && exit 1

RUN mkdir -p /rootfs/bin /rootfs/sbin /rootfs/usr/sbin /rootfs/usr/bin /rootfs/usr/local/bin
RUN cp $(which busybox.static) /rootfs/bin/busybox \
	&& chroot /rootfs /bin/busybox --install -s

RUN mkdir -p /rootfs/etc/network
RUN echo -e 'auto eth0\niface eth0 inet dhcp' > /rootfs/etc/network/interfaces

########################################################
# busybox builder
########################################################
FROM alpine-busybox-ref AS busybox-static-builder
RUN apk add --no-cache build-base wget tar linux-headers musl-dev musl-utils perl
WORKDIR /build

ARG BUSYBOX_VERSION

# Grab and unpack BusyBox source
RUN wget -qO- https://busybox.net/downloads/busybox-${BUSYBOX_VERSION}.tar.bz2 \
	| tar -xj
WORKDIR /build/busybox-${BUSYBOX_VERSION}

# 1  baseline defconfig
RUN make defconfig

# 2  apply overrides from repo
COPY busybox.fragment.config .config.fragment
RUN KCONFIG_ALLCONFIG=.config.fragment yes "" | make oldconfig

ARG TARGET=aarch64
RUN make -j$(nproc) CFLAGS="-static -fno-PIE" LDFLAGS="-static -no-pie"

RUN mkdir -p /rootfs/bin /rootfs/sbin /rootfs/usr/sbin /rootfs/usr/bin /rootfs/usr/local/bin
RUN cp busybox /rootfs/bin/ \
	&& chroot /rootfs /bin/busybox --install -s

RUN mkdir -p /rootfs/etc/network
RUN echo -e 'auto eth0\niface eth0 inet dhcp' > /rootfs/etc/network/interfaces


########################################################
# jq builder
########################################################

FROM debian-ref AS jq-static-builder

ARG JQ_GIT_REF

RUN git clone --depth 1 --branch ${JQ_GIT_REF} https://github.com/jqlang/jq.git /app
## have to skip building docs but I don't need them for my use case
RUN cd /app \
	&& git submodule init \
	&& git submodule update \
	&& autoreconf -i \
	&& ./configure --disable-valgrind --enable-all-static --disable-docs --disable-maintainer-mode --prefix=/usr/local \
	&& make -j$(nproc) LDFLAGS=-all-static \
	&& strip jq

RUN cd /app && make check || echo "check failed"

########################################################
# lshw builder
########################################################

FROM debian-ref AS lshw-static-builder

ARG LSHW_GIT_REF

RUN git clone --depth 1 --branch ${LSHW_GIT_REF} https://github.com/lyonel/lshw.git /src
WORKDIR /src
# Build statically linked lshw
RUN make static
RUN strip src/lshw-static
RUN cp src/lshw-static /bin/lshw

########################################################
# socat builder
########################################################

FROM alpine/socat:${SOCAT_VERSION} AS socat-static-builder

########################################################
# e2fsprogs builder
########################################################

FROM alpine-ref AS e2fsprogs-static-builder

ARG E2FSPROGS_GIT_REF

RUN apk add --no-cache build-base git autoconf automake linux-headers musl-dev
RUN git clone --depth 1 --branch ${E2FSPROGS_GIT_REF} https://git.kernel.org/pub/scm/fs/ext2/e2fsprogs.git /src
WORKDIR /src
# Pass STATIC LDFLAGS so no interpreter is needed
RUN CFLAGS='-O2' \
	LDFLAGS='-static -s' \
	./configure --enable-static --disable-shared
RUN make -j$(nproc)
RUN strip misc/mke2fs
RUN cp misc/mke2fs /bin/

########################################################
# crun builder
########################################################

# Stage 1: Build with Nix
FROM nixos/nix AS crun-static-builder-nix

# Clone & build via the nix expression
RUN git clone --recursive https://github.com/containers/crun.git /src
WORKDIR /src
RUN nix build -f nix -L --extra-experimental-features nix-command

RUN strip ./result/bin/crun
RUN cp ./result/bin/crun /bin/crun

FROM debian-ref AS crun-static-builder-debian

RUN apt-get update && apt-get install -y --no-install-recommends \
	git build-essential autoconf automake libtool pkg-config \
	musl-tools \
	libyajl-dev \
	libseccomp-dev \
	libcap-dev \
	ca-certificates \
	python3 \
	&& rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git clone --recursive https://github.com/containers/crun.git .

# Configure and compile statically against musl
RUN CC=musl-gcc \
	CFLAGS="-O2 -static" \
	LDFLAGS="-static" \
	./autogen.sh \
	&& ./configure --disable-shared \
	&& make

RUN strip /src/crun
RUN cp /src/crun /bin/crun

########################################################
# crun builder
########################################################

FROM alpine-ref AS crun-static-builder-alpine

RUN apk add --no-cache \
	git build-base autoconf automake libtool pkgconf \
	musl-dev go-md2man \
	libcap-dev libseccomp-dev yajl-dev yajl-static argp-standalone \
	python3 libcap-dev libcap-static libseccomp-dev libseccomp-static

WORKDIR /src
RUN git clone --depth 1 --recurse-submodules \
	https://github.com/containers/crun.git . \
	&& git submodule update --init --recursive

RUN ./autogen.sh

RUN CFLAGS="-O2 -static" LDFLAGS="-static" ./configure --disable-shared --disable-systemd --enable-static

RUN make V=1 LDFLAGS="-static -all-static" -j$(nproc)

RUN strip /src/crun
RUN cp /src/crun /bin/crun

########################################################
# runc builder
########################################################

FROM debian-golang-ref AS runc-static-builder

ARG RUNC_GIT_REF

RUN apt-get update \
	&& apt-get install -y \
		build-essential \
		autoconf \
		libtool \
		git \
		bison \
		flex \
		wget \
		libseccomp-dev \
		libseccomp2 \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=forks / /app/

# RUN git clone --depth 1 --branch ${RUNC_GIT_REF} https://github.com/opencontainers/runc.git /src
# WORKDIR /src

WORKDIR /app/runc

RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod <<EOF
make static
EOF

RUN strip runc
RUN cp runc /bin/runc


########################################################
# runc-test builder
########################################################

FROM debian-golang-ref AS runc-test-static-builder

ARG RUNC_GIT_REF

RUN apt-get update \
	&& apt-get install -y \
		build-essential \
		autoconf \
		libtool \
		git \
		bison \
		flex \
		wget \
		libseccomp-dev \
		libseccomp2 \
	&& rm -rf /var/lib/apt/lists/*

ARG WORKDIR_REMAP="/app"

COPY --from=forks / ${WORKDIR_REMAP}
COPY --from=runm . ${WORKDIR_REMAP}/runm/

WORKDIR ${WORKDIR_REMAP}/runm/test/integration/cmd/runc-test

# RUN git clone --depth 1 --branch ${RUNC_GIT_REF} https://github.com/opencontainers/runc.git /src
# WORKDIR /src
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod <<EOF
make static TRIMPATH=
EOF

RUN strip runc
RUN cp runc /bin/runc-test


########################################################
# runm-linux-go-base builder
########################################################

FROM alpine-golang-ref AS runm-linux-go-base-builder

COPY --from=forks / /app/
COPY --from=runm . /app/runm

WORKDIR /app/runm

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

########################################################
# runm-linux-init builder
########################################################

FROM runm-linux-go-base-builder AS runm-linux-init-builder

WORKDIR /app/runm

ARG ARCH 

RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod <<EOF
GOOS=linux GOARCH=${ARCH} go build -x -ldflags="-s -w -extldflags=-static" -trimpath -o /out/runm-linux-init ./cmd/runm-linux-init
EOF

########################################################
# runm-linux-init builder
########################################################

FROM runm-linux-go-base-builder AS runm-linux-host-fork-exec-proxy-builder

WORKDIR /app/runm

ARG ARCH 

RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod <<EOF
GOOS=linux GOARCH=${ARCH} go build -x -ldflags="-s -w -extldflags=-static" -trimpath -o /out/runm-linux-host-fork-exec-proxy ./cmd/runm-linux-host-fork-exec-proxy
EOF

########################################################
# initramfs builder
########################################################

FROM scratch AS initramfs-files

# our static binaries (as needed)
# COPY --from=socat-static-builder /usr/bin/socat /mbin/socat
# COPY --from=e2fsprogs-static-builder /bin/mke2fs /mbin/mke2fs
# COPY --from=lshw-static-builder /bin/lshw /mbin/lshw
# COPY --from=runc-static-builder /bin/runc /mbin/runc
# COPY --from=runm-linux-init-builder /out/runm-linux-init /mbin/runm-linux-init
COPY --from=runm-linux-mounter-builder /out/runm-linux-mounter /runm-linux-mounter

# busybox initalized filesystem
COPY --from=busybox-static-builder /rootfs/bin/busybox /bin/busybox

# our raw files
COPY udhcpc.default /rootfs/etc/udhcpc/default.script
COPY resolv.conf /rootfs/etc/resolv.conf

########################################################
# mbin builder
########################################################

FROM scratch AS mbin-files

# our static binaries (as needed)
# COPY --from=socat-static-builder /usr/bin/socat /mbin/socat
# COPY --from=e2fsprogs-static-builder /bin/mke2fs /mbin/mke2fs
# COPY --from=lshw-static-builder /bin/lshw /mbin/lshw
COPY --from=runc-static-builder /bin/runc /runc
COPY --from=crun-static-builder-alpine /bin/crun /crun
COPY --from=runm-linux-init-builder /out/runm-linux-init /runm-linux-init
COPY --from=runm-linux-host-fork-exec-proxy-builder /out/runm-linux-host-fork-exec-proxy /runm-linux-host-fork-exec-proxy
COPY --from=runc-test-static-builder /bin/runc-test /runc-test

########################################################
# initramfs cpio builder
########################################################

FROM alpine-ref AS initramfs-cpio-builder
RUN apk add --no-cache cpio gzip
WORKDIR /work

COPY --from=initramfs-files / /rootfs

RUN chroot /rootfs /bin/busybox ln -s /runm-linux-mounter /init

RUN mkdir -p /dist \
	&& (cd /rootfs && find . -print0 | cpio --null -ov --format=newc > /dist/initramfs.cpio) \
	&& gzip -1 /dist/initramfs.cpio


########################################################
# mbin squashfs builder
########################################################

FROM alpine-ref AS mbin-squashfs-builder
RUN apk add --no-cache squashfs-tools
WORKDIR /work

RUN mkdir -p /dist

COPY --from=mbin-files / /rootfs

RUN mksquashfs /rootfs /dist/mbin.squashfs


########################################################
# export stage
########################################################

FROM scratch AS export
COPY --from=initramfs-cpio-builder /dist/initramfs.cpio.gz /initramfs.cpio.gz
COPY --from=mbin-squashfs-builder /dist/mbin.squashfs /mbin.squashfs


FROM scratch AS busybox-export
COPY --from=busybox-static-builder /rootfs/bin/busybox /busybox
