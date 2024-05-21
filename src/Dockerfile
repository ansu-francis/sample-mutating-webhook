FROM golang:1.22.3-alpine as build-env

WORKDIR /app

COPY go.mod /go.sum /app/
RUN go mod download

COPY . /app/

RUN CGO_ENABLED=0 go build -o /webhook

FROM alpine:3.16 as runtime

COPY --from=build-env /webhook /usr/local/bin/webhook
RUN chmod +x /usr/local/bin/webhook

ENTRYPOINT ["webhook"]
