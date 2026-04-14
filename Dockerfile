FROM golang:1.25 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/tcloud-public-csi-driver ./cmd/tcloud-public-csi-driver

FROM debian:bookworm-slim

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
