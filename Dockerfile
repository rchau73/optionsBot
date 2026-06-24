# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder
WORKDIR /build
# Download deps first (cached layer when source changes but go.mod/go.sum don't)
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o bot ./cmd/bot

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.20
# ca-certificates: required for TLS connections to Deribit WS
# tzdata: required for correct timezone handling in expiry/DTE calculations
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/bot ./bot
# data/ is created at runtime (persists state.json, backtest results, etc.)
RUN mkdir -p data
ENTRYPOINT ["./bot"]
