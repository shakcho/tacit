FROM node:26.13.1-alpine
ARG SOURCE_REVISION=dev
LABEL org.opencontainers.image.source="https://github.com/shakcho/tacit" \
      org.opencontainers.image.revision="${SOURCE_REVISION}"
WORKDIR /app
COPY --chown=node:node apps/web/package.json apps/web/README.md ./
USER node
CMD ["node", "--version"]
