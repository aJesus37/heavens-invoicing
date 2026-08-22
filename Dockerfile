# syntax=docker/dockerfile:1
# Multi-stage build for the invoice-app static binary.
# go.mod requires go 1.26.0; use the matching golang:1.26 toolchain.

# ---- build stage ----
FROM golang:1.26 AS build
WORKDIR /src

# Cache module downloads separately from the source tree.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a statically linked, no-CGO, linux/amd64 binary.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/invoice-app .

# ---- runtime stage ----
# Matches the current production deployment: a single static binary on scratch.
FROM scratch

# CA certificates so outbound HTTPS works (Telegram Bot API, WhatsApp/whatsmeow,
# SMTP STARTTLS). Without these, crypto/tls has no roots to verify against.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/invoice-app /invoice-app

# The app stores its SQLite DB under ./data relative to the working directory.
# Running from "/" makes ./data resolve to /data, where the volume is mounted.
WORKDIR /
ENV PORT=8010
EXPOSE 8010
CMD ["/invoice-app"]
