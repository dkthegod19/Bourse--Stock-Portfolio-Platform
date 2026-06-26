# Multi-stage build producing two small binaries (api + worker) from one image.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
# Resolve dependencies and generate go.sum if it isn't committed yet.
RUN go mod tidy
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -o /out/worker ./cmd/worker

FROM alpine:3.20
RUN apk add --no-cache ca-certificates postgresql-client
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /out/worker /app/worker
COPY migrations /app/migrations
COPY ui /app/ui
ENV UI_DIR=/app/ui
EXPOSE 8080
# Default to the API; compose overrides the worker service command.
CMD ["/app/api"]
