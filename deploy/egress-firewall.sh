#!/bin/sh
# Egress firewall for yoli.
#
# This runs inside a small sidecar container that owns a network namespace;
# the yoli container joins it via `network_mode: "service:netfw"`, so these
# rules govern *all* of yoli's traffic at the IP layer (every protocol, not
# just HTTP). Policy:
#
#   * all outbound INTERNET is allowed
#   * the local/private network ranges are blocked (host LAN, cloud metadata)
#   * no inbound connections (only replies to connections yoli itself opened)
#
# BRIDGE_SUBNET must match the Docker network subnet in
# docker-compose.egress.yml — traffic to the gateway and embedded DNS lives
# there and has to be allowed for the internet to work at all.

set -eu

BRIDGE_SUBNET="${BRIDGE_SUBNET:-172.31.255.0/24}"

# Start from a clean slate so re-runs are idempotent.
iptables -F
iptables -X 2>/dev/null || true

# --- INPUT: no inbound except replies to our own connections ---------------
iptables -P INPUT DROP
iptables -A INPUT -i lo -j ACCEPT
iptables -A INPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT

# --- OUTPUT: allow internet, deny local/private destinations ---------------
iptables -P OUTPUT ACCEPT
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A OUTPUT -d 127.0.0.0/8 -j ACCEPT
# Allow the local bridge (gateway + Docker embedded DNS) so we can get out.
iptables -A OUTPUT -d "$BRIDGE_SUBNET" -j ACCEPT
# Block the rest of the private / link-local space.
iptables -A OUTPUT -d 10.0.0.0/8 -j REJECT
iptables -A OUTPUT -d 172.16.0.0/12 -j REJECT
iptables -A OUTPUT -d 192.168.0.0/16 -j REJECT
iptables -A OUTPUT -d 169.254.0.0/16 -j REJECT
iptables -A OUTPUT -d 100.64.0.0/10 -j REJECT
# Everything else (public internet) falls through to the ACCEPT policy.

# Drop IPv6 entirely if the stack is present, to avoid an unfiltered path.
if command -v ip6tables >/dev/null 2>&1; then
  ip6tables -P INPUT DROP 2>/dev/null || true
  ip6tables -P OUTPUT DROP 2>/dev/null || true
  ip6tables -A OUTPUT -o lo -j ACCEPT 2>/dev/null || true
  ip6tables -A INPUT -i lo -j ACCEPT 2>/dev/null || true
fi

echo "egress-firewall: applied (bridge=$BRIDGE_SUBNET)"

# Hold the network namespace open for the lifetime of the stack.
exec sleep infinity
