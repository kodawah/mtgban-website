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
FROM alpine:3.19 AS build-release-stage

RUN mkdir -p /app/bantu
WORKDIR /app/bantu

RUN apk update && apk add --no-cache sudo bash curl xz jq

# Create and execute the script in one RUN command to reduce layers
RUN echo $'#!/bin/sh\n\
curl -O "https://mtgjson.com/api/v5/AllPrintings.json.xz"\n\
xz -dc AllPrintings.json.xz | jq > /tmp/allprintings5.json.new\n\
if [ $? -eq 0 ]; then\n\
    mv /tmp/allprintings5.json.new ./allprintings5.json\n\
fi\n\
rm AllPrintings.json.xz\n' > get-mtgjson.sh && \ 
chmod +x get-mtgjson.sh && \ 
./get-mtgjson.sh

COPY --from=build /mtgbantu-website .
COPY /templates ./templates
COPY /css ./css
COPY /js ./js
COPY /img ./img

EXPOSE 8080

ENTRYPOINT ["/app/bantu/mtgbantu-website"]
CMD []
