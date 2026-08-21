# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /agent-fanout ./cmd/main.go

# Runtime stage
#
# No USER directive, deliberately: a GitHub Actions container action runs as
# root because the runner mounts GITHUB_WORKSPACE with an ownership the action
# has to write through. A non-root USER loses that access.
FROM alpine:3.24

# ca-certificates only. This action makes HTTPS calls to api.github.com from a
# CGO_ENABLED=0 static binary, so crypto/x509 needs a system trust store — but
# it never shells out, and `git` came in from the template. Every git operation
# in this project happens in the workflow's own steps, outside this container.
RUN apk add --no-cache ca-certificates

COPY --from=builder /agent-fanout /usr/local/bin/agent-fanout

ENTRYPOINT ["agent-fanout"]
