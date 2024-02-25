# First stage: build the Go binary
FROM golang:1.19 AS build

RUN go env -w GO111MODULE=auto

RUN mkdir /src

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . /src

RUN CGO_ENABLED=0 GOOS=linux go build -o /mtgbantu-website -v -x

# Second stage: Run Go binary
FROM alpine:latest AS build-release-stage

RUN apk update && apk add --no-cache sudo

RUN mkdir -p /app/bantu

WORKDIR /app/bantu

COPY --from=build /mtgban-website .
COPY --from=build /src/config.json .
COPY --from=build /src/credentials.json .
COPY --from=build /src/creds.json .
COPY --from=build /src/allprintings5.json .
COPY --from=build /src/templates ./templates
COPY --from=build /src/css ./css
COPY --from=build /src/js ./js
COPY --from=build /src/img ./img

EXPOSE 8050

ENTRYPOINT ["/app/bantu/mtgbantu-website"]
CMD []