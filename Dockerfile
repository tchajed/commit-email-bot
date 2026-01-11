# syntax=docker/dockerfile:1
# https://www.docker.com/blog/containerize-your-go-developer-environment-part-2/
# NOTE: not using the golang alpine image since we need cgo for sqlite3. It doesn't matter much since this image is only
# used during the build; the running container is the Python one.
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN --mount=type=cache,target=/root/.cache/go-build go build -v -o /out/commit-email-bot .

FROM debian:trixie-slim
WORKDIR /app
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends git curl ca-certificates ssh git-delta aha; \
    apt-get clean; \
    rm -rf /var/lib/apt/lists/*

# Pre-populate SSH known_hosts with common git hosting services
RUN mkdir -p /root/.ssh && \
    ssh-keyscan github.com gitlab.com bitbucket.org >> /root/.ssh/known_hosts

# Install dotenvx
RUN curl -sfS https://dotenvx.sh/install.sh | sh

# Copy the Go binary built from the build stage
COPY --from=build /out/commit-email-bot .

COPY .env.production ./

EXPOSE 8888
EXPOSE 80
ENV TLS_HOSTNAME="commit-emails.xyz"
CMD [ "dotenvx", "run", "--", "/app/commit-email-bot", "-port", "8888", "-log", "commit-email-bot.log" ]
