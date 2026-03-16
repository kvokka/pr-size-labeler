FROM debian:bookworm-slim

RUN useradd --create-home --shell /usr/sbin/nologin appuser

WORKDIR /app
COPY pr-size /app/pr-size

ENV LISTEN_ADDR=:7860
EXPOSE 7860

USER appuser
ENTRYPOINT ["/app/pr-size"]
