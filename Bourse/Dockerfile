# Multi-stage build: build the React frontend, then the two Go binaries
# (api + worker), then assemble a small runtime image.

# 1) Frontend (Vite + React + Tailwind) -> static dist/
FROM node:20-alpine AS web
WORKDIR /web
COPY frontend/package.json ./
RUN npm install
COPY frontend/ ./
RUN npm run build

# 2) Go binaries
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
# Resolve dependencies and generate go.sum if it isn't committed yet.
RUN go mod tidy
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -o /out/worker ./cmd/worker

# 3) Runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates postgresql-client
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /out/worker /app/worker
COPY migrations /app/migrations
COPY --from=web /web/dist /app/ui
ENV UI_DIR=/app/ui
EXPOSE 8080
# Default to the API; compose overrides the worker service command.
CMD ["/app/api"]
