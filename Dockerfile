# First stage: build the Go binary
FROM golang:1.19 AS build

RUN go env -w GO111MODULE=auto

RUN mkdir /src

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . /src

WORKDIR /src

RUN CGO_ENABLED=0 GOOS=linux go build -o /mtgban-website -v -x

# Second stage: Run Go binary
FROM alpine:latest AS build-release-stage

RUN apk update && apk add --no-cache sudo

RUN mkdir /app/bantu

WORKDIR /app/bantu

COPY --from=build mtgban-website .
COPY --from=build config.json .
COPY --from=build credentials.json .
COPY --from=build creds.json .
COPY --from=build allprintings5.json .
COPY --from=build /templates ./templates
COPY --from=build /css ./css
COPY --from=build /js ./js
COPY --from=build /img ./img
ENTRYPOINT ["/app/bantu/mtgban-website", "-dev","true","-skip","true", "-sig", "false"]
CMD []