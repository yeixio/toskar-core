# Clustering

Yggdrasil can use more than one computer. Each computer runs `yggdrasil-daemon`. Work is placed per role. One model is not split across machines.

## Discovery

Discovery is enabled by default. The daemon advertises `_localai._tcp` and browses for the same service. Peers show up through `GET /api/v1/nodes` and the Computers page.

The service's port is Bifrost's (7332). Its TXT record carries `node_id`, `name`, `version`, `pairing`, and `api_port`, the port of the API (7331 by default), so an app that finds Yggdrasil on the network, such as the iPhone app, knows where to connect.

When mDNS cannot see peers, set static addresses:

- `static_peers` in `config.json`, as `host:7332`
- or `YGGDRASIL_STATIC_PEERS=host-a:7332,host-b:7332`

`docker-compose.cluster.yml` uses static peers and `YGGDRASIL_STUB_INFERENCE=true`. That compose file checks pairing and placement without a real GGUF. It is not a GPU cluster.

If discovery is on at startup and Bifrost is still bound to loopback, the daemon sets the internal host to `0.0.0.0`. Changing discovery later can require a restart. Settings expose that as `discovery_needs_restart`.

## Pairing

Pairing is a consent step between two daemons.

1. On one computer, start pairing against a discovered node (`POST /api/v1/nodes/pair` or the web UI).
2. On the other computer, approve the offer (`POST /api/v1/nodes/{id}/pair/approve`).
3. Each side stores the peer identity. Later Bifrost calls that read models or run chat send a bearer token checked against that peer's certificate.

`/internal/v1/health`, `/internal/v1/node`, and the pairing routes answer without that token. Other internal routes reject a missing or invalid token.

Revoke a peer with `POST /api/v1/nodes/{id}/revoke`.

## Placement

Norn (`internal/scheduler`) scores candidates and picks a node for a role. The Team orchestrator runs three roles in order: coordinator, worker, reviewer. With two paired computers and models installed where those roles need them, those roles can land on different machines. The event stream records `scheduler.placement`.

A manual pass is written up in [two-machine-team-demo.md](two-machine-team-demo.md). Continuous integration does not run that pass on physical hardware. The Docker cluster check uses stub inference.

## Ports

| Port | Bind by default | Role |
| --- | --- | --- |
| 7331 | `127.0.0.1` | Web UI, `/api/v1`, `/v1` |
| 7332 | `0.0.0.0` when discovery is on | Bifrost |

Keep 7331 on loopback unless you have read [privacy.md](privacy.md) and [SECURITY.md](../SECURITY.md). A non-loopback bind requires an API key and the daemon will not start without one. Firewall prompts on macOS are about local-network access for discovery, not about publishing the API to the internet.
