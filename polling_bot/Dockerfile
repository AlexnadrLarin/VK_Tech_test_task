FROM golang:1.24 as builder

WORKDIR /polling_bot_build

COPY ../go.mod ../go.sum ./
RUN go mod download

COPY ../ .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o polling_bot_exec ./cmd/bot/

RUN go test -v ./...

FROM alpine:3.18 as release

WORKDIR /polling_bot

COPY --from=builder /polling_bot_build/polling_bot_exec .

CMD ["./polling_bot_exec"]
