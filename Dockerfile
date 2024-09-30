# syntax=docker/dockerfile:1
# https://www.docker.com/blog/containerize-your-go-developer-environment-part-2/
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY mailbot.go index.html ./
RUN --mount=type=cache,target=/root/.cache/go-build go build -v -o /out/commit-email-bot .

FROM python:3.12-slim
WORKDIR /app
RUN set -eux; \
  apt-get update; \
  apt-get install -y --no-install-recommends git curl; \
  apt-get clean; \
  rm -rf /var/lib/apt/lists/*

# Install dotenvx
RUN curl -sfS https://dotenvx.sh/install.sh | sh
COPY .env.production ./

# Copy the Go binary built from the build stage
COPY --from=build /out/commit-email-bot .
COPY git_multimail_wrapper.py requirements.txt ./
RUN pip3 install -r requirements.txt

EXPOSE 8888
EXPOSE 80
ENV TLS_HOSTNAME="www.commit-emails.xyz"
CMD [ "dotenvx", "run", "--", "/app/commit-email-bot", "-port", "8888" ]
