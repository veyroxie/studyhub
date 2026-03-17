FROM golang:1.24-alpine AS builder

WORKDIR /app
# context = project root, so backend/ prefix is needed
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN CGO_ENABLED=0 GOOS=linux go build -o studyhub .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates postgresql16-client bash s3cmd && \
    addgroup -g 1000 studyhub && adduser -D -u 1000 -G studyhub studyhub
WORKDIR /app
COPY --from=builder /app/studyhub .
# Serve frontend from the project root's frontend/ folder
COPY frontend/ ./frontend/
COPY backend/backup.sh ./backup.sh
RUN chmod +x /app/backup.sh && \
    mkdir -p /app/uploads /app/backups && chown -R studyhub:studyhub /app/uploads /app/backups /app

USER 1000:1000
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -qO- http://localhost:8080/api/auth/me || exit 0
CMD ["./studyhub"]
