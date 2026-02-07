# Build stage
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=$(date -u '+%Y-%m-%d_%H:%M:%S') -X main.GitCommit=${GIT_COMMIT}" \
    -o /clawreachbridge \
    ./cmd/clawreachbridge

# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -H -s /sbin/nologin clawreachbridge

COPY --from=builder /clawreachbridge /usr/local/bin/clawreachbridge

USER clawreachbridge

EXPOSE 8080 8081

ENTRYPOINT ["clawreachbridge"]
CMD ["start", "--config", "/etc/clawreachbridge/config.yaml"]
