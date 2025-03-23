FROM golang:1.21-alpine

WORKDIR /app

# Копируем файлы зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем приложение
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# Открываем порт
EXPOSE 8080

# Запускаем приложение
CMD ["./main"] 