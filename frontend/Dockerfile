# ── Build ─────────────────────────────────────────────────────────────────────
FROM node:24-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
RUN NEXT_OUTPUT=standalone npm run build

# ── Run: Next.js standalone server, non-root ──────────────────────────────────
FROM node:24-alpine
WORKDIR /app
ENV NODE_ENV=production PORT=3000 HOSTNAME=0.0.0.0
COPY --from=builder --chown=node:node /app/.next/standalone ./
COPY --from=builder --chown=node:node /app/.next/static ./.next/static
# Users and sessions (data/monitor/auth.db): compose mounts a volume here; the
# folder is created owned by node so a new volume starts writable.
RUN mkdir -p /app/data/monitor && chown -R node:node /app/data && chmod 700 /app/data/monitor
USER node
EXPOSE 3000
CMD ["node", "server.js"]
