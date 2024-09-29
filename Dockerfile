# syntax=docker/dockerfile:1
# https://www.docker.com/blog/containerize-your-go-developer-environment-part-2/
FROM golang:latest
WORKDIR /src
COPY . .
RUN go mod download

RUN go build -o /app

EXPOSE 8888
ENV TLS_HOSTNAME="commit-emails.xyz"
ENV WEBHOOK_SECRET=""
CMD [ "/app", "-port", "8888" ]
