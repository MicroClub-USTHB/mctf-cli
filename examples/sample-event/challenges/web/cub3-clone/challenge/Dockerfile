FROM python:3.12-alpine

WORKDIR /app
RUN adduser -D ctf && \
    pip install --no-cache-dir flask gunicorn

COPY src/ /app/

USER ctf
EXPOSE 8000

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD wget -q -O - http://localhost:8000/ >/dev/null || exit 1

CMD ["gunicorn", "-b", "0.0.0.0:8000", "app:app"]
