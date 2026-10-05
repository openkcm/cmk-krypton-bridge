# ---- Build Stage ----
FROM golang:1.27-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/cmk-krypton-bridge ./cmd/proxy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/cmk2kryptonctl ./cmd/cli

# ---- Runtime Stage ----
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /bin/cmk-krypton-bridge /cmk-krypton-bridge
COPY --from=builder /bin/cmk2kryptonctl     /cmk2kryptonctl

EXPOSE 9092 8080
USER nonroot:nonroot
ENTRYPOINT ["/cmk-krypton-bridge"]
