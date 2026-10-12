# Image for the mctf CLI itself (not a challenge image).
# Built and published by goreleaser; the binary is supplied by the build
# context, so this does not compile anything.
#
# git + ssh are included because `mctf build`/`deploy` shell out to them;
# docker/kubectl are expected to be provided by the CI runner (e.g. a mounted
# docker socket) rather than baked in.
FROM alpine:3.20

RUN apk add --no-cache ca-certificates git openssh-client

COPY mctf /usr/local/bin/mctf

ENTRYPOINT ["/usr/local/bin/mctf"]
