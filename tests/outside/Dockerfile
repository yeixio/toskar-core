# One image for every machine in the outside-network check: the router
# (NAT and miniupnpd), the relay, Core, and the clients. Their programs are
# mounted in, built by the test.
FROM alpine:3.22
RUN apk add --no-cache iproute2 iptables iptables-legacy miniupnpd curl ca-certificates
