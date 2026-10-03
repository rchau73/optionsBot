# ── Build stage ───────────────────────────────────────────────────────────────
# Keep the Go version on a supported, patched release: the TLS stack that talks
# to Deribit comes from the standard library compiled into the binary.
FROM golang:1.26-alpine AS builder
WORKDIR /build
# Download deps first (cached layer when source changes but go.mod/go.sum don't)
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o bot ./cmd/bot

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.22
# ca-certificates: required for TLS connections to Deribit WS
# tzdata: required for correct timezone handling in expiry/DTE calculations
RUN apk --no-cache add ca-certificates tzdata \
 && adduser -D -H -u 10001 bot
WORKDIR /app
COPY --from=builder /build/bot ./bot
# The bot writes everything it keeps (bot.log, orders.log, hedge_report.json,
# data/*.jsonl) relative to its working directory. docker-compose.yml mounts
# one host folder per underlying at /app/run and starts the bot there, so all
# of it survives container rebuilds and the bots never share a file.
RUN mkdir -p run && chown -R bot:bot /app
WORKDIR /app/run
# Never run a trading bot as root.
USER bot
ENTRYPOINT ["/app/bot"]
