FROM node:22.23.3-slim AS build
WORKDIR /project/apps/web
COPY apps/web/package.json apps/web/bun.lock ./
RUN npm install --global bun@1.3.10 && bun install --frozen-lockfile
COPY apps/web/ ./
ENV BUILD_STANDALONE=true NEXT_PUBLIC_SENTRY_DISABLED=true NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM node:22.23.3-slim
WORKDIR /project
ENV NODE_ENV=production HOSTNAME=0.0.0.0 PORT=3000 NEXT_TELEMETRY_DISABLED=1
COPY --from=build --chown=node:node /project/apps/web/.next/standalone ./
COPY --from=build --chown=node:node /project/apps/web/public ./apps/web/public
COPY --from=build --chown=node:node /project/apps/web/.next/static ./apps/web/.next/static
USER node
CMD ["node", "apps/web/server.js"]
