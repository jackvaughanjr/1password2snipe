FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY . .
RUN go build -o /app/1password2snipe .

FROM alpine:3.21
COPY --from=builder /app/1password2snipe /app/1password2snipe
ENTRYPOINT ["/app/1password2snipe"]
