# One image for every machine in the outside-network check: the router
# (NAT and miniupnpd), the relay, Core, and the clients. Their programs are
# mounted in, built by the test. nftables throughout: CI kernels may have
# no legacy iptables tables.
FROM debian:trixie-slim
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      miniupnpd-nftables nftables iproute2 curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*
