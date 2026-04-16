FROM golang:1.26.2-alpine

WORKDIR /app

RUN apk add --no-cache git && \
    go install github.com/air-verse/air@latest

EXPOSE 8080

CMD ["sh", "-c", "go mod download && air"]
