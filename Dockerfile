# syntax=docker/dockerfile:1

# ---- Stage 1: build the SPA ----
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- Stage 2: build the Go binary ----
FROM golang:1.23-alpine AS build
ENV CGO_ENABLED=0 GOOS=linux
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Replace the embed directory with the freshly built SPA.
RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist
COPY --from=web /web/dist/ internal/webui/dist/
ARG BUILD_DATE=""
RUN go build -trimpath \
      -ldflags "-s -w -X github.com/lovelytoaster94/routerhub/internal/admin.BuildDate=${BUILD_DATE}" \
      -o /out/routerhub ./cmd/routerhub

# ---- Stage 3: runtime ----
FROM alpine:3.20
RUN apk add --no-cache tzdata ca-certificates
COPY --from=build /out/routerhub /routerhub
EXPOSE 8080
# WORKDIR is the data volume: SQLite (routerhub.db) is created here.
WORKDIR /data
VOLUME ["/data"]
ENV ROUTERHUB_HOST=0.0.0.0 \
    ROUTERHUB_PORT=8080 \
    TZ=UTC
ENTRYPOINT ["/routerhub"]
