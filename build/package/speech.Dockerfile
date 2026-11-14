FROM python:3.13.18-slim
ARG SOURCE_REVISION=dev
LABEL org.opencontainers.image.source="https://github.com/shakcho/tacit" \
      org.opencontainers.image.revision="${SOURCE_REVISION}"
RUN groupadd --gid 65532 nonroot && useradd --uid 65532 --gid 65532 --no-create-home nonroot
WORKDIR /service
COPY --chown=nonroot:nonroot services/speech/src ./src
USER nonroot:nonroot
CMD ["python", "--version"]
