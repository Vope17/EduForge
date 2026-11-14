# syntax=docker/dockerfile:1
FROM node:22-bookworm-slim AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM node:22-bookworm-slim AS math
WORKDIR /app/math
COPY math/package.json math/package-lock.json ./
RUN npm ci --omit=dev
COPY math/render.mjs ./

FROM golang:1.27-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/eduforge ./cmd/eduforge

FROM node:22-bookworm-slim AS runtime
ARG CODEX_VERSION=0.167.1
ARG CLAUDE_CODE_VERSION=2.1.290
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates poppler-utils libreoffice-impress libreoffice-writer \
    fonts-noto-cjk tini librsvg2-bin \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g @openai/codex@${CODEX_VERSION} @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION} \
    && npm cache clean --force \
    && useradd --create-home --uid 10001 eduforge \
    && mkdir -p /data /secrets/codex /secrets/claude /secrets/keys /workspace \
    && chown -R eduforge:eduforge /data /secrets /workspace
COPY --from=backend /out/eduforge /usr/local/bin/eduforge
COPY --from=web /src/frontend/dist /app/web
COPY --from=math /app/math /app/math
USER eduforge
WORKDIR /workspace
ENV EDUFORGE_DATA=/data EDUFORGE_SECRETS=/secrets EDUFORGE_WEB=/app/web EDUFORGE_CONTAINER=1 \
    CODEX_HOME=/secrets/codex CLAUDE_CONFIG_DIR=/secrets/claude
EXPOSE 8080
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/eduforge"]
CMD ["serve", "--listen", "0.0.0.0:8080"]

# Development and test image: `docker build --target dev -t eduforge-dev .`, then
# `scripts/dev.sh make test`. Not used at runtime.
FROM golang:1.27-bookworm AS dev
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates poppler-utils libreoffice-impress libreoffice-writer \
    fonts-noto-cjk git librsvg2-bin \
    && rm -rf /var/lib/apt/lists/*
COPY --from=node:22-bookworm-slim /usr/local/bin/node /usr/local/bin/node
COPY --from=node:22-bookworm-slim /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
WORKDIR /src
