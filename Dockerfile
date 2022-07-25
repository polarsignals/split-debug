FROM golang:1.18-alpine as builder

RUN apk update && apk add make git

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

WORKDIR /app

COPY go.mod go.sum /app/
RUN go mod download -modcacherw

COPY Makefile /app/
COPY --chown=nobody:nogroup ./main.go ./main.go
COPY --chown=nobody:nogroup ./pkg ./pkg
RUN make build

FROM alpine:3.16.1

USER nobody

COPY --chown=0:0 --from=builder /app/dist/split-debug /split-debug

CMD ["/split-debug"]
