FROM --platform=$BUILDPLATFORM leszko/cuda:11.7.1-cudnn8-devel-ubuntu20.04-${TARGETARCH} as build

ARG	TARGETARCH
ARG	BUILDARCH

ENV	GOARCH="$TARGETARCH" \
	PATH="/usr/local/go/bin:/go/bin:${PATH}" \
	PKG_CONFIG_PATH="/root/compiled/lib/pkgconfig" \
	CPATH="/usr/local/cuda/include" \
	LIBRARY_PATH="/usr/local/cuda/lib64" \
	DEBIAN_FRONTEND="noninteractive" \
	CGO_LDFLAGS="-L/usr/local/cuda/lib64"

RUN	apt update \
	&& apt install -yqq software-properties-common curl apt-transport-https lsb-release yasm \
	&& curl -fsSL https://dl.google.com/go/go1.20.4.linux-${BUILDARCH}.tar.gz | tar -C /usr/local -xz \
	&& curl -fsSL https://download.docker.com/linux/ubuntu/gpg | apt-key add - \
	&& add-apt-repository "deb [arch=${BUILDARCH}] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" \
	&& curl -fsSl https://apt.llvm.org/llvm-snapshot.gpg.key | apt-key add - \
	&& add-apt-repository "deb [arch=${BUILDARCH}] https://apt.llvm.org/$(lsb_release -cs)/ llvm-toolchain-$(lsb_release -cs)-12 main" \
	&& apt update \
	&& apt -yqq install clang-12 clang-tools-12 lld-12 build-essential pkg-config autoconf git python docker-ce-cli pciutils gcc-multilib libgcc-8-dev-arm64-cross gcc-mingw-w64-x86-64

RUN	update-alternatives --install /usr/bin/clang++ clang++ /usr/bin/clang++-12 30 \
	&& update-alternatives --install /usr/bin/clang clang /usr/bin/clang-12 30 \
	&& update-alternatives --install /usr/bin/ld ld /usr/bin/lld-12 30

RUN	GRPC_HEALTH_PROBE_VERSION=v0.3.6 \
	&& curl -fsSL https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-${TARGETARCH} -o /usr/bin/grpc_health_probe \
	&& chmod +x /usr/bin/grpc_health_probe \
	&& ldconfig /usr/local/lib

# note: for runtime, Tensorflow version needs to be compatible with CUDA and CuDNN of the image
RUN	LIBTENSORFLOW_VERSION=2.12.1 \
	&& curl -LO https://storage.googleapis.com/tensorflow/libtensorflow/libtensorflow-gpu-linux-x86_64-${LIBTENSORFLOW_VERSION}.tar.gz \
	&& mkdir /tf && tar -C /tf -xzf libtensorflow-gpu-linux-x86_64-${LIBTENSORFLOW_VERSION}.tar.gz

ENV	GOPATH=/go \
	GO_BUILD_DIR=/build/ \
	GOFLAGS="-mod=readonly"

WORKDIR	/src

RUN	mkdir -p /go \
	&& curl -fsSLO https://github.com/livepeer/livepeer-ml/releases/download/v0.3/tasmodel.pb

COPY	./install_ffmpeg.sh	./install_ffmpeg.sh

ARG	BUILD_TAGS
ENV	BUILD_TAGS=${BUILD_TAGS}

COPY	go.mod	go.sum	./
RUN	go mod download

RUN	./install_ffmpeg.sh \
	&& GO111MODULE=on go get -v github.com/golangci/golangci-lint/cmd/golangci-lint@v1.52.2 \
	&& go get -v github.com/jstemmer/go-junit-report
