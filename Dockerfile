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
FROM gcr.io/distroless/static-debian12

COPY --from=build /out/invoice-app /invoice-app

# The app stores its SQLite DB under ./data relative to the working directory.
# The production volume is mounted at /data, so WORKDIR /data makes ./data
# resolve to /data/data — this is where the live database already lives and
# must stay, otherwise a redeploy would orphan all existing data.
WORKDIR /data
ENV PORT=8010
EXPOSE 8010
CMD ["/invoice-app"]
