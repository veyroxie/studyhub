FROM golang:1.24-alpine AS builder

WORKDIR /app
# context = project root, so backend/ prefix is needed
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.buildVersion=${VERSION}" \
    -o studyhub .

# Build a purged + minified tailwind.css from the current frontend sources.
# Uses the Tailwind v4 standalone CLI (no npm needed). The binary is a
# glibc-linked Rust executable, so we build on debian-slim, not alpine
# (musl) — only the resulting CSS file gets copied into the final image.
FROM debian:stable-slim AS tailwind-builder
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && rm -rf /var/lib/apt/lists/*
RUN curl -fsSLo /usr/local/bin/tailwindcss \
        https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.0/tailwindcss-linux-x64 \
    && chmod +x /usr/local/bin/tailwindcss \
    && /usr/local/bin/tailwindcss --help >/dev/null
WORKDIR /frontend
COPY frontend/ ./
RUN /usr/local/bin/tailwindcss -i tailwind.in.css -o tailwind.css --minify

FROM alpine:3.19
RUN apk add --no-cache ca-certificates postgresql16-client bash s3cmd && \
    addgroup -g 1000 studyhub && adduser -D -u 1000 -G studyhub studyhub
WORKDIR /app
COPY --from=builder /app/studyhub .
# Serve frontend from the project root's frontend/ folder
COPY frontend/ ./frontend/
# Overlay the freshly purged tailwind.css from the dedicated build stage so
# the runtime image always carries CSS that matches the current HTML/JS.
COPY --from=tailwind-builder /frontend/tailwind.css ./frontend/tailwind.css
COPY backend/backup.sh ./backup.sh
RUN chmod +x /app/backup.sh && \
    mkdir -p /app/uploads /app/backups && chown -R studyhub:studyhub /app/uploads /app/backups /app

USER 1000:1000
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -qO- http://localhost:8080/api/auth/me || exit 0
CMD ["./studyhub"]
