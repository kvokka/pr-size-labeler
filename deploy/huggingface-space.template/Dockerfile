FROM debian:trixie-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates=20250419 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /usr/sbin/nologin appuser

WORKDIR /app
COPY pr-size-labeler /app/pr-size-labeler
RUN chmod 0755 /app/pr-size-labeler && chown appuser:appuser /app/pr-size-labeler

ENV LISTEN_ADDR=:7860
EXPOSE 7860

USER appuser
ENTRYPOINT ["/app/pr-size-labeler"]
