FROM golang:1.22-alpine AS builder
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -o /qiuqiu-backend ./cmd/server

FROM ghcr.io/cirruslabs/flutter:stable@sha256:46691e311715845de03a3ba4753a475476936805b29431b1f00f1816981033f8 AS client-builder
USER root
WORKDIR /app/client
COPY client/pubspec.yaml client/pubspec.lock ./
RUN flutter pub get
COPY client/ ./
RUN flutter build web --release

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app/backend
COPY --from=builder /qiuqiu-backend /qiuqiu-backend
COPY backend/prompts ./prompts
COPY backend/migrations ./migrations
COPY client/assets/live2d /app/client/assets/live2d
COPY --from=client-builder /app/client/build/web /app/client/build/web
EXPOSE 8080
CMD ["/qiuqiu-backend"]
