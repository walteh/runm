########################################################
# args
########################################################

ARG BUSYBOX_VERSION=1.36.1
ARG ALPINE_BUSYBOX_VERSION=3.21

FROM alpine:${ALPINE_BUSYBOX_VERSION} AS alpine-busybox-ref

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

FROM scratch AS export
COPY --from=busybox-static-builder /rootfs/bin/busybox /busybox

FROM scratch AS export-with-rootfs
COPY --from=busybox-static-builder /rootfs /rootfs