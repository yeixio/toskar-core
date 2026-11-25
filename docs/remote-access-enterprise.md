# Access from anywhere for organizations

How an organization gives hundreds of people Toskar away from the office:
browsers through its own front door, and phones, tablets, and TVs through a
way in that keeps their encryption end to end. The design is in
[remote-access.md](remote-access.md); this is the guide.

## Pick a way in

| Way in | Browsers | Paired devices | What it needs |
|---|---|---|---|
| **Your own Toskar relay** | No | Yes, end to end | A small server with a public name, and a relay license from Toskar |
| **A VPN** (Tailscale, WireGuard, your corporate VPN) | Yes | Yes, end to end | Everyone on the VPN |
| **A reverse proxy or tunnel** (nginx, Caddy, Traefik, a cloud load balancer, Cloudflare Tunnel) | Yes, with your sign-in | Yes, paired by name (below); the proxy can read the traffic | A public name and certificate |
| **A forwarded port** | No | Yes, end to end | A public address for each Toskar server |

Most organizations pair two of them: a reverse proxy or tunnel for the web
app, where people sign in with your identity provider, and the relay or a
VPN for devices.

**Why devices differ:** a paired device trusts its Toskar server by the
server's own certificate, saved when it was paired. Through the relay, a VPN,
or a forwarded port, that certificate reaches the device unchanged, so the
encryption runs from the device to the server. A reverse proxy or Cloudflare
Tunnel shows its own certificate and ends TLS itself, so it can read the
traffic. Devices accept it only for the name they were paired with, when the
device's system trusts the certificate for that name, as a browser would
(below).

## Your own relay

The relay is Toskar's relay, run on your hardware under your name. Devices
reach a Toskar server through it when nothing more direct works, and it
passes their encrypted bytes along without being able to read them. Toskar
servers need no inbound port. Setting up the relay itself is covered in the
relay's own guide, which comes with your license.

On each Toskar server:

1. Open **Settings › Network access** and turn on **Access from anywhere**.
2. Under **Your organization's relay**, enter the relay's name (such as
   `relay.example.com`) and the enrollment secret from the relay, then
   **Connect**.
3. The line above says **Connected to relay.example.com**.

The server enrolls itself, renews its own token, keeps its sealed address on
the relay, and holds a connection open to it. The enrollment secret is kept
with the server's secrets and never shown again; it's only ever sent to your
relay, never to Toskar's. If the relay's certificate comes from your own CA,
start Toskar with `TOSKAR_RELAY_CA` set to a PEM file of that CA.

Devices learn the relay when they pair, or the next time they're on the
office network. Away, they try the office address, then any direct way in,
then the relay, and **Settings → Computer** on the device says which one
they're using.

## A VPN

Tailscale is the shortest path: install it on the Toskar server and on each
device, and have people pair their devices while on Tailscale. Toskar accepts
Tailscale addresses at pairing as it accepts the office network's, so the
device keeps that address and reaches the server wherever it is. A
corporate VPN that puts devices on the office network works the same way.

## A reverse proxy or tunnel, for the web app

Put Toskar behind your proxy at a name you control, such as
`toskar.example.com`, with a certificate from a public CA or your own:

- **The upstream** is the Toskar API, `http://<server>:7331`, or HTTPS on
  the same port with its own certificate.
- **Sign-in** from your identity provider, either way:
  - **The proxy signs people in** (Authelia, Authentik, Cloudflare Access,
    Tailscale): set `TOSKAR_TRUSTED_PROXIES` to the proxy's address, and
    Toskar takes the person from its `Remote-User` header.
    `TOSKAR_PROXY_ADMIN_GROUPS` and `TOSKAR_PROXY_MEMBER_GROUPS` turn groups
    into roles; `TOSKAR_PROXY_DEFAULT_ROLE` covers everyone else.
  - **Toskar signs people in** with OpenID Connect (Google, Microsoft,
    Okta, Keycloak, …): register `https://toskar.example.com/api/v1/oidc/callback`
    and set `TOSKAR_OIDC_ISSUER`, `TOSKAR_OIDC_CLIENT_ID`, and
    `TOSKAR_OIDC_CLIENT_SECRET`, with `TOSKAR_OIDC_ADMIN_GROUPS` and
    `TOSKAR_OIDC_MEMBER_GROUPS` for roles.
- **Cloudflare Tunnel** with Cloudflare Access needs no inbound port: point
  the tunnel at `http://localhost:7331` on the Toskar server.

The user guide's *Run without a desktop* section has the details.

### Devices through your proxy

A device can use the same name, so it reaches the server through your proxy
when it's away:

- **Pair by name.** On the office network, have people connect their
  device to the server's name, such as `toskar.example.com`, rather than its
  address. Inside, your DNS answers with the server's own address (pairing
  only works on the office network); outside, it answers with the proxy's.
- **The certificate** the proxy serves must be one the devices' systems
  trust for that name: from a public CA, or your own CA installed on them
  by your device management. Renewals need nothing from anyone. The device
  never extends this to an IP address, or to a relay's names, which stay
  pinned to the server's own certificate.
- **Let device requests through.** Devices send their key as
  `Authorization: Bearer …` to `/api/v1/…`. The proxy must pass those
  requests to Toskar without its own login page: a bypass rule for the API
  with a bearer token, or service authentication. Browsers still sign in.
- **The proxy sees the traffic,** since it ends TLS. If that matters, use
  the relay or a VPN for devices instead.

## Hundreds of people

- **People, not devices, are what you manage.** Everyone signs in to the web
  app with your identity provider, gets a role from their groups, and pairs
  their own devices from there (**Connect a device**). Nobody pairs hundreds
  of devices by hand. Removing a person, or one of their devices, revokes it
  at once, on every way in.
- **Pairing happens on the office network or the VPN.** A pairing code works
  only there, so a code that leaks can't be used from outside.
- **Each device's key** reaches only what devices use: chat, its own
  conversations, and the lists it reads. From outside, Toskar answers
  nothing else, and an address that sends twenty wrong keys in ten minutes
  waits before it can try again.
- **Capacity** is about models, not the way in: many people chatting at
  once need models on enough computers. Pair GPU computers into the cluster
  ([clustering.md](clustering.md)) and set placement, effort, and limits per
  profile.
- **Health checks** for a load balancer: `GET /api/v1/health`.
- **Data** lives in the Toskar server's data directory: keep it on durable
  storage and back it up. Run records age out as **Settings → Keep run
  records for** says.

## What leaves your network

- **Through your relay:** encrypted bytes between devices and Toskar
  servers, and sealed address records only paired devices can open. Your
  relay, and nothing of Toskar's, sees them.
- **Through a VPN or your proxy:** whatever your VPN or proxy sees; with a
  proxy that ends TLS, that's the traffic itself.
- **To Toskar:** nothing. A self-hosted relay checks its license on its own
  and never calls home.
