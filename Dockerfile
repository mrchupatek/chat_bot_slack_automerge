FROM golang:1.23.5 AS builder

WORKDIR /app

COPY . /app
RUN go mod tidy

RUN go build -o chat_bot

FROM golang:1.23.5

WORKDIR /app
COPY --from=builder /app/chat_bot .
COPY .env /app/.env
RUN mkdir -p /app/storage/
COPY storage/automerge_example.db /app/storage/automerge.db

ENTRYPOINT ["./chat_bot"]
