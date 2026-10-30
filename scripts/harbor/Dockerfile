FROM mirror.gcr.io/library/alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
RUN apk add --no-cache python3 nodejs bash jq git
ARG BINARY=gh-aw-linux-amd64
COPY ${BINARY} /usr/local/bin/gh-aw
COPY scripts/harbor/verify.py /tests/verify.py
COPY scripts/harbor/task/tests/ /tests/
RUN chmod 0555 /usr/local/bin/gh-aw /tests/test.sh
WORKDIR /tests
