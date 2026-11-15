FROM golang:1.27.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG SOURCE_REVISION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${SOURCE_REVISION}" -o /out/tacit-db ./cmd/tacit-db

FROM gcr.io/distroless/static-debian13:nonroot
ARG SOURCE_REVISION=dev
LABEL org.opencontainers.image.source="https://github.com/shakcho/tacit" \
      org.opencontainers.image.revision="${SOURCE_REVISION}"
COPY --from=build --chown=nonroot:nonroot /out/tacit-db /tacit-db
USER nonroot:nonroot
ENTRYPOINT ["/tacit-db"]
