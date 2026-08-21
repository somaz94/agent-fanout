# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /agent-fanout ./cmd/main.go

# Runtime stage
FROM alpine:3.24

RUN apk add --no-cache git ca-certificates

COPY --from=builder /agent-fanout /usr/local/bin/agent-fanout

ENTRYPOINT ["agent-fanout"]
