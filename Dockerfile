# syntax=docker/dockerfile:1.7

FROM golang:1.23-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd

ARG OCI_SOURCE=""
ARG OCI_REVISION=""
ARG OCI_CREATED=""

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${OCI_REVISION}" \
    -o /out/homebox-location-label-service \
    ./cmd/homebox-location-label-service

FROM scratch

ARG OCI_SOURCE=""
ARG OCI_REVISION=""
ARG OCI_CREATED=""

LABEL org.opencontainers.image.title="HomeboxLocationLabelService" \
      org.opencontainers.image.description="Small Homebox external label service that renders location-only PNG labels with a QR/DataMatrix code." \
      org.opencontainers.image.source="${OCI_SOURCE}" \
      org.opencontainers.image.revision="${OCI_REVISION}" \
      org.opencontainers.image.created="${OCI_CREATED}"

USER 65532:65532
COPY --from=build /out/homebox-location-label-service /homebox-location-label-service

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD ["/homebox-location-label-service", "--healthcheck"]
ENTRYPOINT ["/homebox-location-label-service"]
