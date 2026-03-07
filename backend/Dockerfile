FROM golang:1.21-alpine AS builder

WORKDIR /app
# context = project root, so backend/ prefix is needed
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN CGO_ENABLED=0 GOOS=linux go build -o studyhub .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/studyhub .
# Serve frontend from the project root's frontend/ folder
COPY frontend/ ./frontend/

EXPOSE 8080
CMD ["./studyhub", "-db", "/data/studyhub.db"]
