FROM golang:1.20

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.buildStamp=`date +'%Y-%m-%d_%H:%M.%S'` -X main.gitHash=`git rev-parse --short HEAD`"

FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /root

COPY --from=0 /app .

CMD [ "./entrypoint.sh" ]