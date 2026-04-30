FROM golang:1.21-alpine AS builder

WORKDIR /app

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Сначала копируем только файлы модулей
COPY go.mod go.sum ./



# Затем копируем весь код и собираем
COPY main/*.go ./
#RUN go build -o main .

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
    -ldflags="-s -w -extldflags '-static'" \
    -trimpath \
    -o main .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
RUN adduser -D -u 1001 appuser
USER appuser
WORKDIR /root/
COPY --from=builder --chown=appuser:appuser /app/main .

CMD ["./main"]