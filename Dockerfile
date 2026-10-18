FROM alpine:3

RUN apk add --no-cache ca-certificates

COPY octodcp /usr/local/bin/octodcp

ENTRYPOINT ["octodcp"]
