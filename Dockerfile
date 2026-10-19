FROM golang:1.26.2-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY . ./

ARG TARGET=.
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/pulseroute "$TARGET"

FROM alpine:3.22

RUN addgroup -S pulseroute && adduser -S -G pulseroute pulseroute
COPY --from=build /out/pulseroute /usr/local/bin/pulseroute

USER pulseroute
ENTRYPOINT ["/usr/local/bin/pulseroute"]