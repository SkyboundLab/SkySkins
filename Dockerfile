FROM golang:latest AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o skyskins .

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/skyskins .

EXPOSE 8080

CMD ["./skyskins"]
