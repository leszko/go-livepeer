FROM --platform=$BUILDPLATFORM leszko/go-livepeer-builder:${TARGETARCH} as build

ARG	TARGETARCH
ARG	BUILDARCH

ENV	GOARCH="$TARGETARCH" \
	PATH="/usr/local/go/bin:/go/bin:${PATH}" \
	PKG_CONFIG_PATH="/root/compiled/lib/pkgconfig" \
	CPATH="/usr/local/cuda/include" \
	LIBRARY_PATH="/usr/local/cuda/lib64" \
	DEBIAN_FRONTEND="noninteractive" \
	CGO_LDFLAGS="-L/usr/local/cuda/lib64"

ARG	BUILD_TAGS
ENV	BUILD_TAGS=${BUILD_TAGS}

COPY	.	.

RUN	make livepeer livepeer_cli livepeer_bench livepeer_router

FROM --platform=$TARGETPLATFORM	nvidia/cuda:11.7.1-cudnn8-runtime-ubuntu20.04 AS livepeer-amd64-base

FROM --platform=$TARGETPLATFORM	nvidia/cuda:11.7.1-cudnn8-runtime-ubuntu20.04 AS livepeer-arm64-base

FROM	livepeer-${TARGETARCH}-base

ENV	NVIDIA_DRIVER_CAPABILITIES=all

COPY --from=build	/build/	/usr/local/bin/
COPY --from=build	/usr/bin/grpc_health_probe	/usr/local/bin/grpc_health_probe
COPY --from=build	/src/tasmodel.pb	/tasmodel.pb
COPY --from=build	/usr/share/misc/pci.ids	/usr/share/misc/pci.ids

# libtensorflow.so is required at runtime, because Ffmpeg DNN filter loads it dynamically
COPY --from=build	/tf/	/usr/local/

ENTRYPOINT	["/usr/local/bin/livepeer"]
