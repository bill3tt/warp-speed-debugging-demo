FROM golang:1.17-alpine3.14 as builder

RUN apk add --update --no-cache ca-certificates tzdata git make bash && update-ca-certificates

ADD . /opt
WORKDIR /opt

RUN git update-index --refresh; make build

FROM alpine:3.14 as runner

COPY --from=builder /opt/warp-speed-debugging-demo /bin/warp-speed-debugging-demo

ENTRYPOINT ["/bin/warp-speed-debugging-demo"]