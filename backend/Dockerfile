FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

FROM alpine:3.19
# Runtime deps:
#   ca-certificates, tzdata, wget — base + healthcheck
#   libreoffice                  — .pptx -> .pdf
#   poppler-utils                — pdftoppm (.pdf -> .png)
#   libwebp-tools                — cwebp (.png -> .webp)
#   ttf-dejavu, ttf-liberation   — avoid boxes for missing fonts in LibreOffice
RUN apk --no-cache add \
    ca-certificates tzdata wget \
    libreoffice poppler-utils libwebp-tools \
    ttf-dejavu ttf-liberation
WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/pkg/badwords/badwords.json pkg/badwords/badwords.json
COPY --from=builder /app/migrations migrations

EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q --spider http://localhost:8080/health || exit 1

CMD ["./server"]
