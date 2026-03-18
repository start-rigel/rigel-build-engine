FROM golang:1.22-alpine AS builder
WORKDIR /app
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.google.cn
COPY go.mod ./
COPY go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN go build -o /rigel-service ./cmd/server

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /rigel-service /usr/local/bin/rigel-service
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/rigel-service"]
