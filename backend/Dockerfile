FROM node:24-alpine AS fonts
WORKDIR /assets
COPY frontend/download-fonts.mjs ./
RUN node download-fonts.mjs

# Codex CLI signs in to ChatGPT and runs generation turns. Only its native
# binary is copied out, so the backend images need no Node.js.
FROM node:24-alpine AS codex
ARG CODEX_VERSION=0.167.1
RUN npm install -g @openai/codex@${CODEX_VERSION} \
    && cp -R "$(npm root -g)"/@openai/codex/node_modules/@openai/codex-linux-*/vendor/*-unknown-linux-musl /opt/codex \
    && /opt/codex/bin/codex --version

FROM golang:1.27.4-bookworm AS development
WORKDIR /app
ENV CGO_ENABLED=0
RUN apt-get update && apt-get install -y --no-install-recommends libreoffice-writer libreoffice-impress fontconfig poppler-utils \
    && rm -rf /var/lib/apt/lists/*
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
COPY --from=fonts /assets/public/fonts /app/fonts
COPY --from=fonts /assets/public/fonts /usr/share/fonts/truetype/eduforge
RUN fc-cache -f
COPY --from=codex /opt/codex /opt/codex
ENV PATH=/opt/codex/bin:/opt/codex/codex-path:$PATH CODEX_HOME=/codex
RUN mkdir -p /codex
EXPOSE 8080
CMD ["go", "run", "./cmd/server"]

FROM development AS build
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM debian:bookworm-slim AS production
# Preserve the UID/GID that owns data volumes created by the Alpine image.
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget libreoffice-writer libreoffice-impress fontconfig \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 101 app && useradd --system --uid 100 --gid 101 app \
    && mkdir /data /codex && chown app:app /data /codex
WORKDIR /app
COPY --from=build /out/server ./server
COPY --from=fonts /assets/public/fonts /app/fonts
COPY --from=fonts /assets/public/fonts /usr/share/fonts/truetype/eduforge
RUN fc-cache -f
COPY --from=codex /opt/codex /opt/codex
ENV PATH=/opt/codex/bin:/opt/codex/codex-path:$PATH CODEX_HOME=/codex HOME=/codex
USER app
EXPOSE 8080
ENTRYPOINT ["/app/server"]

