FROM golang:1.22-alpine AS builder
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -o /qiuqiu-backend ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app/backend
COPY --from=builder /qiuqiu-backend /qiuqiu-backend
COPY backend/prompts ./prompts
COPY backend/migrations ./migrations
COPY client/assets/live2d /app/client/assets/live2d
EXPOSE 8080
CMD ["/qiuqiu-backend"]
