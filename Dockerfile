FROM golang:1.23.5 AS builder

WORKDIR /app

COPY . /app
RUN go mod tidy

RUN go build -o chat_bot

FROM golang:1.23.5

WORKDIR /app
COPY --from=builder /app/chat_bot .
COPY .env /app/.env

ENTRYPOINT ["./chat_bot"]
