FROM golang:1.16-buster as builder

# Set necessary environmet variables needed for our image
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

ADD . /src

RUN cd /src && go build -o snoopy snoopy.go

FROM alpine

WORKDIR /app
COPY --from=builder /src/snoopy /app/

EXPOSE 3000/tcp

ENTRYPOINT ./snoopy