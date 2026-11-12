FROM golang:1.27.4-alpine AS development
WORKDIR /app
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
EXPOSE 8080
CMD ["go", "run", "./cmd/server"]

FROM development AS build
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.22 AS production
RUN addgroup -S app && adduser -S -G app app \
    && apk add --no-cache ca-certificates \
    && mkdir /data && chown app:app /data
WORKDIR /app
COPY --from=build /out/server ./server
USER app
EXPOSE 8080
ENTRYPOINT ["/app/server"]

