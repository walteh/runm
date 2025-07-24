########################################################
# args
########################################################

ARG LINUX_KERNEL_VERSION=6.15.8

ARG CONFIG_VAR_TO_VALIDATE=CONFIG_OVERLAY_FS

########################################################
# builder
########################################################

FROM debian:bookworm-slim AS builder

RUN apt-get update && apt-get install -y \
	git \
	make \
	gcc \
	flex \
	bison \
	libssl-dev \
	libelf-dev \
	bc \
	kmod \
	cpio \
	&& rm -rf /var/lib/apt/lists/*

ARG LINUX_KERNEL_VERSION

ENV SRC_DIR=/usr/src \
	DIST_DIR=/dist \
	LINUX_DIR=/usr/src/linux \
	LINUX_REPO_URL=git://git.kernel.org/pub/scm/linux/kernel/git/stable/linux-stable.git

RUN mkdir -p ${SRC_DIR} ${DIST_DIR} \
	&& git clone --depth 1 --branch v${LINUX_KERNEL_VERSION} ${LINUX_REPO_URL} ${LINUX_DIR}

WORKDIR ${LINUX_DIR}

ARG TARGETARCH
ARG CONFIG_VAR_TO_VALIDATE

# 1. Set the build target
ENV ARCH=${TARGETARCH}
#ENV CROSS_COMPILE=${TARGETARCH}-linux-gnu-   # if needed

# 2. Copy in your config fragments
COPY base.config ./
COPY ${TARGETARCH}.fragment.config ./

# RUN make ARCH=${TARGETARCH} defconfig


# 3. Merge them into a working .config
RUN scripts/kconfig/merge_config.sh -m base.config ${TARGETARCH}.fragment.config

RUN grep ${CONFIG_VAR_TO_VALIDATE} .config

RUN <<EOF
if [ "${TARGETARCH}" = "arm64" ]; then
	make ARCH=${TARGETARCH} LOCALVERSION= -j$(nproc) Image
else
	make ARCH=${TARGETARCH} LOCALVERSION= -j$(nproc) bzImage
fi
EOF

RUN grep ${CONFIG_VAR_TO_VALIDATE} .config

RUN <<EOF
if [ "${TARGETARCH}" = "arm64" ]; then
	cp arch/arm64/boot/Image /boot/kernel
	cp .config /boot/config
else
	cp arch/x86/boot/bzImage /boot/kernel
	cp .config /boot/config
fi
EOF

RUN grep ${CONFIG_VAR_TO_VALIDATE} .config

########################################################
# export stage
########################################################

FROM scratch AS export
COPY --from=builder /boot /
