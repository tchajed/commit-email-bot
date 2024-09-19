#syntax=docker/dockerfile:1.2
# https://www.docker.com/blog/containerize-your-go-developer-environment-part-2/
FROM golang:latest as build
WORKDIR /src
COPY go.mod go.sum /src/
RUN go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build go build -v -o /out/app .

FROM scratch AS bin
COPY --from=build /out/app /
