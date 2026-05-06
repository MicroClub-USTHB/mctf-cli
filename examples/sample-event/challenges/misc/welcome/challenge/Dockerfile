FROM python:3.12-alpine

WORKDIR /app
RUN adduser -D ctf
COPY src/ /app/

USER ctf
EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget -q -O - http://localhost:8000/ >/dev/null || exit 1

CMD ["python", "-m", "http.server", "8000"]
