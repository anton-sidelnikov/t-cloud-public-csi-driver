FROM golang:1.25 AS builder

WORKDIR /src

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
	-ldflags "-X t-cloud-public-csi-driver/internal/version.Version=${VERSION} -X t-cloud-public-csi-driver/internal/version.Commit=${COMMIT} -X t-cloud-public-csi-driver/internal/version.Date=${BUILD_DATE}" \
	-o /out/tcloud-public-csi-driver ./cmd/tcloud-public-csi-driver

FROM debian:bookworm-slim

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="T Cloud Public CSI Driver" \
	org.opencontainers.image.description="CSI driver for T Cloud Public storage backends" \
	org.opencontainers.image.version="${VERSION}" \
	org.opencontainers.image.revision="${COMMIT}" \
	org.opencontainers.image.created="${BUILD_DATE}"

RUN apt-get update \
	&& apt-get install -y --no-install-recommends \
		ca-certificates \
		e2fsprogs \
		mount \
		util-linux \
		xfsprogs \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/tcloud-public-csi-driver /usr/local/bin/tcloud-public-csi-driver

ENTRYPOINT ["/usr/local/bin/tcloud-public-csi-driver"]
