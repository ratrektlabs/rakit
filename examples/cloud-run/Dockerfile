FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /server .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /server /server

ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/server"]
