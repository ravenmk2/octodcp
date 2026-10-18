FROM alpine:3.24.2

RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 octodcp \
    && adduser -D -u 10001 -G octodcp octodcp \
    && mkdir -p /app/data \
    && chmod 700 /app/data \
    && chown -R octodcp:octodcp /app

WORKDIR /app
COPY --chmod=0555 --chown=octodcp:octodcp octodcp /app/octodcp

USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/octodcp"]
CMD ["serve"]
