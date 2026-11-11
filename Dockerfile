FROM golang:1.21-alpine AS builder

WORKDIR /app

# Сначала копируем только файлы модулей
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Затем копируем весь код и собираем
COPY main/*.go ./
#RUN go build -o main .

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
    -ldflags="-s -w -extldflags '-static'" \
    -trimpath \
    -o main .

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
RUN adduser -D -u 1001 appuser
WORKDIR /app
COPY --from=builder --chown=appuser:appuser /app/main ./main
RUN mkdir -p /app/data && chown -R appuser:appuser /app
USER appuser

CMD ["./main"]