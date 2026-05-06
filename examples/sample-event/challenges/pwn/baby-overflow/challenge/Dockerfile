FROM ubuntu:22.04 AS build
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc make ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY src/ /src/
RUN make

FROM redpwn/jail:0.3.1
COPY --from=build /src/chal /srv/app/run
COPY src/flag.txt /srv/app/flag.txt
ENV JAIL_TIME=120 \
    JAIL_MEM=20M \
    JAIL_PIDS=10 \
    JAIL_CONNS_PER_IP=4
EXPOSE 5000
USER 1000:1000
