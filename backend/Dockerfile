FROM golang:1.24-alpine AS builder

WORKDIR /app
# context = project root, so backend/ prefix is needed
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X studyhub/internal/core.BuildVersion=${VERSION}" \
    -o studyhub ./cmd/api

# Build a purged + minified tailwind.css from the current frontend sources,
# plus minify every .js file in place so the final image ships ~280KB of
# compressed JS instead of the ~732KB raw source.
#
# Uses the Tailwind v4 standalone CLI + esbuild standalone — no npm needed.
# Both binaries are glibc-linked, so we build on debian-slim and only the
# resulting (minified) files get copied into the alpine runtime image.
FROM debian:stable-slim AS tailwind-builder
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates tar gzip && rm -rf /var/lib/apt/lists/*
RUN curl -fsSLo /usr/local/bin/tailwindcss \
        https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.0/tailwindcss-linux-x64 \
    && chmod +x /usr/local/bin/tailwindcss \
    && /usr/local/bin/tailwindcss --help >/dev/null
# esbuild standalone — published as an npm package whose tarball contains a
# pre-built native binary. No Node runtime needed at build OR run.
RUN curl -fsSLo /tmp/esbuild.tgz https://registry.npmjs.org/@esbuild/linux-x64/-/linux-x64-0.24.0.tgz \
    && mkdir -p /tmp/esbuild && tar -xzf /tmp/esbuild.tgz -C /tmp/esbuild --strip-components=1 \
    && cp /tmp/esbuild/bin/esbuild /usr/local/bin/esbuild \
    && chmod +x /usr/local/bin/esbuild \
    && rm -rf /tmp/esbuild /tmp/esbuild.tgz \
    && /usr/local/bin/esbuild --version
WORKDIR /frontend
COPY frontend/ ./
RUN /usr/local/bin/tailwindcss -i tailwind.in.css -o tailwind.css --minify
# Minify every .js in place — preserves the file structure (lazy
# analytics.js, defer order) but cuts parse + transfer cost. --allow-overwrite
# is needed because input and output are the same path. --target=es2019
# matches the dev tools / mobile browsers we support without bloating polyfills.
RUN find js -name '*.js' -type f -exec \
        /usr/local/bin/esbuild {} --minify --allow-overwrite --target=es2019 --outfile={} \;
# styles.css intentionally NOT minified by esbuild — its CSS parser
# rejects some of our @keyframes patterns. 30KB raw is acceptable and
# Tailwind (the bulk of CSS) is already minified separately.

FROM alpine:3.19
# tzdata is required so the app can resolve TZ=Asia/Kuala_Lumpur — without it
# the binary falls back to UTC and every date (attendance, invoice createdOn,
# early-bird day-of-month) is wrong for the 00:00–08:00 MYT window.
RUN apk add --no-cache ca-certificates tzdata postgresql16-client bash s3cmd && \
    addgroup -g 1000 studyhub && adduser -D -u 1000 -G studyhub studyhub
WORKDIR /app
COPY --from=builder /app/studyhub .
# Serve frontend from the project root's frontend/ folder
COPY frontend/ ./frontend/
# Overlay the freshly purged tailwind.css from the dedicated build stage so
# the runtime image always carries CSS that matches the current HTML/JS.
COPY --from=tailwind-builder /frontend/tailwind.css ./frontend/tailwind.css
# Overlay the minified JS from the builder so the runtime image carries
# the smallest possible bundle.
COPY --from=tailwind-builder /frontend/js ./frontend/js
COPY backend/backup.sh ./backup.sh
COPY backend/backup_verify.sh ./backup_verify.sh
RUN chmod +x /app/backup.sh /app/backup_verify.sh && \
    mkdir -p /app/uploads /app/backups && chown -R studyhub:studyhub /app/uploads /app/backups /app

USER 1000:1000
EXPOSE 8080
# Hit the unauthenticated /api/health endpoint and actually fail on error.
# The old check probed /api/auth/me (always 401) and ended in "|| exit 0",
# so the container reported healthy even when the API was down.
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -qO- http://localhost:8080/api/health || exit 1
CMD ["./studyhub"]
