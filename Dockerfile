# syntax=docker/dockerfile:1

# Build with Go in an isolated stage so the runtime contains only okfok.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . ./

ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' \
      -o /out/okfok ./cmd/okfok

# Static distroless is a minimal, shell-free, non-root runtime image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build --chown=nonroot:nonroot /out/okfok /usr/local/bin/okfok

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/okfok"]
