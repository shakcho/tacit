FROM node:26.13.1-alpine AS dependencies
WORKDIR /workspace
RUN corepack enable && corepack prepare pnpm@10.33.2 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/package.json
COPY packages/contracts/package.json packages/contracts/package.json
RUN pnpm install --frozen-lockfile

FROM dependencies AS builder
ARG SOURCE_REVISION=dev
COPY apps/web apps/web
COPY packages/contracts packages/contracts
COPY internal/adapters/httpapi/openapi.yaml internal/adapters/httpapi/openapi.yaml
RUN pnpm --filter @tacit/web generate \
    && pnpm --filter @tacit/web build

FROM node:26.13.1-alpine AS runtime
ARG SOURCE_REVISION=dev
LABEL org.opencontainers.image.source="https://github.com/shakcho/tacit" \
      org.opencontainers.image.revision="${SOURCE_REVISION}"
ENV HOSTNAME=0.0.0.0 \
    NODE_ENV=production \
    PORT=3000
WORKDIR /app
RUN addgroup --system --gid 1001 nodejs \
    && adduser --system --uid 1001 --ingroup nodejs nextjs
COPY --from=builder --chown=nextjs:nodejs /workspace/apps/web/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /workspace/apps/web/.next/static ./apps/web/.next/static
USER 1001:1001
EXPOSE 3000
CMD ["node", "apps/web/server.js"]
