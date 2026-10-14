#!/bin/sh
# The home router in the outside-network check: NAT from the home network
# to its outside network, and, by MODE, how a port reaches Core from outside.
#   pcp      miniupnpd answering PCP and NAT-PMP only
#   upnp     miniupnpd answering UPnP-IGD only
#   none     nothing opens a port
#   forward  a port forwarded by hand: outside FORWARD_PORT to FORWARD_TO
# LAN_NET and WAN_NET are the two networks' prefixes; UPSTREAM, when set, is
# the next router out (a carrier's NAT).
set -eu

# miniupnpd 2.3 writes its rules with libiptc, the legacy tables, so the
# router uses those throughout.
iptables() { iptables-legacy "$@"; }

iface() { ip -o -4 addr show | awk -v n="$1" 'index($4, n) == 1 { print $2; exit }'; }
lan_if="$(iface "$LAN_NET")"
wan_if="$(iface "$WAN_NET")"
wan_ip="$(ip -o -4 addr show dev "$wan_if" | awk '{ split($4, a, "/"); print a[1]; exit }')"
[ -n "$lan_if" ] && [ -n "$wan_if" ] || { echo "router: no interface on $LAN_NET or $WAN_NET" >&2; exit 1; }
# The way out is the next router, or none: the outside network is the
# whole internet here.
ip route del default 2>/dev/null || true
if [ -n "${UPSTREAM:-}" ]; then
  ip route replace default via "$UPSTREAM" dev "$wan_if"
fi

iptables -t nat -A POSTROUTING -o "$wan_if" -j MASQUERADE
echo "router: $lan_if -> $wan_if ($wan_ip), mode ${MODE:-none}"

case "${MODE:-none}" in
forward)
  iptables -t nat -A PREROUTING -i "$wan_if" -p tcp --dport "$FORWARD_PORT" -j DNAT --to-destination "$FORWARD_TO"
  ;;
pcp | upnp)
  natpmp=no upnp=no
  [ "$MODE" = pcp ] && natpmp=yes
  [ "$MODE" = upnp ] && upnp=yes
  cat >/etc/miniupnpd/miniupnpd.conf <<CONF
ext_ifname=$wan_if
listening_ip=$lan_if
http_port=5000
enable_natpmp=$natpmp
enable_upnp=$upnp
secure_mode=yes
system_uptime=yes
uuid=6c1d1a9e-5b4f-4c2e-9d7a-0d1f2e3c4b5a
allow 1024-65535 0.0.0.0/0 1024-65535
deny 0-65535 0.0.0.0/0 0-65535
CONF
  # miniupnpd's chains, hooked in from the outside interface.
  iptables -t nat -N MINIUPNPD
  iptables -t nat -I PREROUTING -i "$wan_if" -j MINIUPNPD
  iptables -t nat -N MINIUPNPD-POSTROUTING
  iptables -t nat -I POSTROUTING -o "$wan_if" -j MINIUPNPD-POSTROUTING
  iptables -t filter -N MINIUPNPD
  iptables -t filter -I FORWARD -i "$wan_if" ! -o "$wan_if" -j MINIUPNPD
  exec miniupnpd -d -f /etc/miniupnpd/miniupnpd.conf
  ;;
esac
exec sleep infinity
