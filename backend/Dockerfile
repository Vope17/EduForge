FROM node:24-alpine AS fonts
WORKDIR /assets
COPY frontend/download-fonts.mjs ./
RUN node download-fonts.mjs

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
EXPOSE 8080
CMD ["go", "run", "./cmd/server"]

FROM development AS build
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM debian:bookworm-slim AS production
# Preserve the UID/GID that owns data volumes created by the Alpine image.
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget libreoffice-writer libreoffice-impress fontconfig \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 101 app && useradd --system --uid 100 --gid 101 app \
    && mkdir /data && chown app:app /data
WORKDIR /app
COPY --from=build /out/server ./server
COPY --from=fonts /assets/public/fonts /app/fonts
COPY --from=fonts /assets/public/fonts /usr/share/fonts/truetype/eduforge
RUN fc-cache -f
USER app
EXPOSE 8080
ENTRYPOINT ["/app/server"]

