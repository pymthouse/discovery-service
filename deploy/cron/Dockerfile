FROM alpine:3.24
RUN apk add --no-cache curl
COPY refresh.sh /refresh.sh
RUN chmod +x /refresh.sh
ENTRYPOINT ["/refresh.sh"]
