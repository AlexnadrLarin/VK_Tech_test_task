FROM golang:1.24.1-alpine3.21 as builder

RUN apk update && \
    apk add --no-cache \
    openssl-dev \
    pkgconfig \
    gcc \
    musl-dev

WORKDIR /polling_bot_build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o polling_bot_exec ./cmd/bot/

RUN go test -v ./...

FROM alpine:3.21 as release

WORKDIR /polling_bot

COPY --from=builder /polling_bot_build/polling_bot_exec .

CMD ["./polling_bot_exec"]