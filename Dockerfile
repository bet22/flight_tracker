FROM golang:1.21-alpine AS builder

WORKDIR /app

# Сначала копируем только файлы модулей
COPY go.mod go.sum ./
RUN go mod download

# Затем копируем весь код и собираем
COPY main/ .
RUN go build -o main .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/main .
#EXPOSE 8080
CMD ["./main"]