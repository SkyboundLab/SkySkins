FROM golang:latest

WORKDIR /app

COPY . .

RUN go mod download

RUN go build -o skyskins .

EXPOSE 8080

CMD ["./skyskins"]
