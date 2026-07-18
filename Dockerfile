FROM golang:1.26-alpine3.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o skyskins .

FROM alpine:3.24

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/skyskins /app/skyskins

RUN adduser -D -g '' skyskins

USER skyskins
WORKDIR /app

EXPOSE 8080

CMD ["./skyskins"]
