FROM httpd:2.4-alpine@sha256:0136c2d4462f3b8ecc92bea70efdfef4d06523999ae8d7aa533969dea6db4576

RUN apk add --no-cache gettext

# Build context is repo root (Railway / CI).
COPY deploy/apache/httpd.conf.template /usr/local/apache2/conf/httpd.conf.template
COPY deploy/apache/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Railway sets PORT; default 8080 for local smoke tests
ENV PORT=8080
ENV DISCOVERYD_UPSTREAM=http://discoveryd.railway.internal:8088
ENV LB_REGION=local

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=15s --retries=3 \
  CMD sh -c 'wget -q --spider "http://127.0.0.1:${PORT:-8080}/healthz" || exit 1'

ENTRYPOINT ["/entrypoint.sh"]
