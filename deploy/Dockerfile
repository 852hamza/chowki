# The Chowki gateway: a static binary on a distroless base image, run as a
# non-root user. Build it from the repository's root folder:
#
#   docker build -f deploy/Dockerfile -t chowki:dev .
#
# or with `make docker`. docs/get-started/install.md explains how to run it.

# Go runs on the build platform and cross-compiles, so images for other
# platforms build without emulation. Images are pinned by digest; the tag
# says which version the digest is.
FROM --platform=$BUILDPLATFORM golang:1.27.1-trixie@sha256:433790e515d27dc6003e847e644cc0af956985cf315c1c58a3b73ee2dd305183 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=""
ARG DATE=""
# The SQLite driver is pure Go, so the binary needs no C library; -s -w
# leave out the symbol tables, a third of its size.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    make build VERSION="$VERSION" COMMIT="$COMMIT" DATE="$DATE" EXTRA_LDFLAGS="-s -w"
# The configuration, readable by all, and the data folder, whose owner
# and mode a new volume mounted on it copies. The modes are set here, as
# COPY's chmod option would give its mode to the folders it creates too.
RUN install -D -m 0644 deploy/chowki.yaml /out/etc/chowki/chowki.yaml && \
    install -d -m 0700 /out/var/lib/chowki
# The licenses of Chowki and of the modules in its binary, which the image
# ships as they ask.
RUN --mount=type=cache,target=/go/pkg/mod \
    go run ./tools/licenses --out /out/usr/share/licenses/chowki/third_party_licenses && \
    install -m 0644 LICENSE NOTICE /out/usr/share/licenses/chowki/

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
ARG VERSION=dev
ARG COMMIT=""
ARG DATE=""
LABEL org.opencontainers.image.title="Chowki" \
      org.opencontainers.image.description="A self-hosted AI gateway." \
      org.opencontainers.image.source="https://github.com/852hamza/chowki" \
      org.opencontainers.image.documentation="https://github.com/852hamza/chowki/tree/main/docs" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT" \
      org.opencontainers.image.created="$DATE"
COPY --from=build /src/bin/chowki /usr/local/bin/chowki
COPY --from=build /out/etc/chowki /etc/chowki
COPY --from=build /out/usr/share/licenses/chowki /usr/share/licenses/chowki
# 65532 is the nonroot user of the distroless images.
# Copying the folder's parent keeps the folder's mode.
COPY --from=build --chown=65532:65532 /out/var/lib/ /var/lib/
ENV CHOWKI_CONFIG=/etc/chowki/chowki.yaml
WORKDIR /var/lib/chowki
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/chowki"]
CMD ["serve"]
