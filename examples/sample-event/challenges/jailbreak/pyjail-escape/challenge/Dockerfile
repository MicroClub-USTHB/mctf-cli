FROM python:3.12-alpine

WORKDIR /app
RUN adduser -D ctf
COPY src/ /app/

USER ctf
EXPOSE 8000

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD nc -z 127.0.0.1 8000 || exit 1

CMD ["python", "app.py"]
