# Access from anywhere: design

Status: in progress (#456). The remote listener, its setting, router port mapping, the route secret, the address record, and the computer's side of the relay (enrollment, registration, and the tunnel) have shipped in core; the rendezvous and relay are built in the private `yeixio/toskar-relay`. Receipt exchange, hosting Toskar's relay, and the apps are what's left.

The phone, tablet, TV, and watch apps reach a person's own Toskar from anywhere, not only on the home network, with no port forwarding, VPN, or dynamic DNS to set up. Like Plex remote access, the connection is direct whenever it can be, and Toskar's service mostly introduces the two ends. Everything stays end-to-end encrypted to the computer the app already trusts.

## What exists today

- **Pairing** (Connect a device, #216) gives the app a device key that reaches only the routes a phone uses (`auth.DeviceMayReach`), and the API certificate's fingerprint, `sha256:<hex>` of the certificate (`auth.CertFingerprint`).
- **The pin** (#213, #405): the app keeps that fingerprint per `host:port` (`mobile/modules/tls-pins`, keychain key `toskar.tlsFingerprint`). For a pinned host and port, the certificate must match the pin; the system's name and CA checks don't apply. A changed certificate stops the app and asks the person, never falls back to plain HTTP.
- **The certificate** is persistent (`secrets/api-tls.*`, or one the person supplies), so the pin holds across restarts.
- **Who a request is from:** a request from another machine always needs a key; one through a proxy is never "this computer" or "the local network" (`auth.FromThisComputer`, `auth.FromLocalNetwork`).
- **Do it yourself:** Tailscale, a port forward, or Cloudflare Tunnel already work, because pairing accepts their addresses. These stay free and documented.

## The paths, in the order the app tries them

1. **Home network:** the address from pairing, as today. Free, never metered.
2. **Direct:** a public address the computer can be reached at: an IPv6 address, or an IPv4 port the computer opened on the router itself (UPnP-IGD, NAT-PMP, or PCP), or one the person forwarded by hand. Plain HTTPS from the app to the computer.
3. **Relay:** when the computer can't be reached directly (carrier-grade NAT, double NAT, a router that refuses port mapping), the app connects through Toskar's relay, which forwards the encrypted bytes without reading them.

The app tries them at once where it can, and keeps the first that answers with the pinned certificate: the LAN address and the direct ones race, and the relay starts after a short delay (about a second). It remembers which worked and tries that first next time. The current path shows in the app: "At home", "Direct", or "Through Toskar's relay (slower)".

**Hole punching is left out of the first version.** The apps talk HTTPS through the platform's own stack (URLSession, OkHttp), which can't do TCP simultaneous open or a UDP transport such as QUIC over a punched hole without a native networking layer of its own. With direct and relayed paths both end to end, hole punching is an optimization for relay cost, which is small for chat (see Cost). Revisit it if relayed traffic turns out large.

## Setting it up: one switch, one subscribe

For a subscriber it should be as easy as Plex: no ports, addresses, or accounts.

- **In the app,** once it's paired at home, a card offers **Use Toskar away from home**. Tapping it shows the store's subscription sheet, and that's all. The app hands the subscription to the computer over its existing connection, and the computer turns access from anywhere on and finds its own way out: a router port, IPv6, or the relay. The card turns into "Ready away from home".
- **Turning it on from the app** takes an Admin or the Owner, since it changes what the computer serves. Anyone else is told who can turn it on.
- **On the computer,** Settings → Access from anywhere is one switch with a status line. Nothing to type unless the person wants a port they forwarded by hand.
- **Away from home** there's nothing to do: the app tries its paths and keeps the first that works, switching between Wi-Fi and mobile data on its own.
- **When something's wrong,** the app and Settings say what in plain words, and what to do: "Your computer is offline or asleep", "Your router didn't open a port, so Toskar's relay is in use", "The subscription ended".

## End-to-end encryption on every path

Every path is the same TLS session, from the app to the computer's API listener, checked against the same pin:

- **Direct:** the app pins `<public address>:<port>` to the fingerprint it already has.
- **Relay:** the app connects to `<route>.relay.toskar.ai:4443` (the server name it sends) and pins that host and port to the same fingerprint. Port 4443, not 443, because Toskar's relay shares the website's load balancer, whose 443 ends TLS for the website. The relay reads only the server name in the TLS ClientHello, picks the computer's tunnel by it, and passes the bytes through. It never terminates TLS, has no certificate for the computer, and can't read prompts, answers, files, or the device key inside.

A relay or rendezvous that lies, or is taken over, can at most drop or delay traffic: a connection it sends anywhere else fails the pin. The pin is never learned from the service; it only comes from pairing.

## The computer's side

When the person turns on **Settings → Access from anywhere** (off by default; Admins and the Owner only):

- **A remote listener.** The computer serves the device routes, and only those, on a separate port for traffic from outside: phone keys only (`DeviceMayReach`), never "this computer" or "the local network", with stricter rate limits and the same TLS certificate. The full control API, API Access keys, and settings stay on the LAN. Turning the setting off closes it at once.
- **Port mapping:** it asks the router for a mapping to that port with PCP, then NAT-PMP, then UPnP-IGD, renews it before it lapses, and removes it when the setting is turned off or Toskar quits. A port the person forwarded by hand can be entered instead.
- **Public addresses:** its global IPv6 addresses, and the router's external IPv4 address from the mapping protocol; with neither, it asks the rendezvous what address it saw.
- **Registration:** every few minutes, and whenever an address changes, it sends the rendezvous an address record (below).
- **Relay tunnel:** whenever it has a route token, it keeps one outbound connection to the relay, over TLS to the relay's own certificate, and accepts the streams the relay passes it as connections to the remote listener, each with the device's address so the remote listener's limits count the device. No inbound port is needed. An idle tunnel costs only a ping every 25 seconds, and keeping it up means a device can switch to the relay the moment a direct path fails.
- **Status in Settings:** "Reachable from anywhere: direct", "through IPv6", "through Toskar's relay", or "not reachable, and why" (no mapping, no subscription, the service unreachable), with the address and port it found.

## Is it working? The outside check

API Access shows whether devices away from home can reach this computer,
as the relay finds it from the internet (#546), like Plex's "fully
accessible outside your network". A router that says it opened a port can
still be unreachable: a second router in front of it (double NAT), a
firewall, or a provider that blocks inbound ports. So the check doesn't
trust it.

- **What's tried:** the computer asks its relay (toskar-relay's
  `POST /v1/routes/{route}/check`) to reach it through its tunnel, and at
  each direct way in it would advertise. Each way counts only when TLS
  completes to this computer's certificate and `/api/v1/health` answers, as
  a paired device requires.
- **When:** every 30 minutes, after the relay connects or the ways in
  change, and on **Check now**.
- **What it shows:** green when any way works, red when none does, and a
  row per way with what failed and what to do.

| Result | What it means | What to do |
|---|---|---|
| `timeout` | Nothing answered from the internet | Another router in front (double NAT) or a firewall blocks it; use the relay, or forward the port on the outer router too |
| `refused` | The address answered, but nothing listens on the port | Forward the port to this computer |
| `wrong_computer` | Another device answered on the port | Point the forward at this computer |
| `tls`, `http` | Something other than Toskar answered | Free the port, or choose another one |
| `not_public` | The address isn't on the internet | Use a public address, or the relay |
| `no_tunnel` | This computer isn't connected to the relay | See the relay's state on the same card |

The relay dials only public addresses, checks only this computer's
certificate and health path, answers pass or fail, and takes one check a
minute per computer, so the check can't be used to look at anything else.

## The rendezvous

A small stateless service with a key-value store. It holds no traffic.

- **The route ID.** At pairing, the computer gives the app a random 32-byte **route secret** (one per computer, kept with its secrets) beside the fingerprint. The route ID is `HKDF(route secret, "toskar route id")`; the record key is `HKDF(route secret, "toskar route key")`. Devices paired before this get the secret over the home network, with their device key, the first time they connect there.
- **The address record**, sent by the computer:
  - its addresses and ports, and a time;
  - signed with the API certificate's private key, with the certificate included;
  - encrypted with the record key (AES-GCM).
- **Lookup:** the app asks for the record by route ID, decrypts it, checks that the certificate's fingerprint is its pin and the signature holds, and only then uses the addresses.
- **What it learns:** route IDs, opaque blobs, and the computer's and apps' IP addresses as they connect. Not the fingerprint, not the addresses inside the record, not who paired with whom. Someone who learns a fingerprint can't find the computer, because the route ID comes from the secret, not the fingerprint.
- **Entitlement:** a lookup or relay use needs an active subscription for that route ID (below).

## The relay

- **Routing by server name:** the first label of `<route>.relay.toskar.ai` is the route ID (32 characters, one DNS label). The computer's tunnel registers under it, authenticated by the route token the app obtained (below), so only the computer the subscription was bought for can take the route.
- **Pass-through:** the relay copies bytes between the app's TCP connection and a stream in the computer's tunnel, nothing more.
- **Limits:** a fair-use cap on relayed bytes per route a month, connection-rate limits per route and per client address, and an idle timeout. Over the cap, the app says so and keeps the home network and on-device answers.
- **Counting** is by bytes and connections; the relay can't count messages, because it can't read them. That's one reason for flat pricing (below).

## Subscription and entitlement

- **The subscription is bought in the app** with App Store or Play billing. No account with us: the store receipt is the entitlement.
- **Receipt exchange:** the app sends the receipt and the route ID to the rendezvous, which checks it with the App Store Server API or the Google Play Developer API and returns a **route token**: route ID, expiry, signed by the service. The app passes the token to the computer over its existing TLS session, and the computer presents it when registering and opening its tunnel.
- **Renewals, cancellations, refunds** reach the service through the stores' server notifications; an expired token stops new relay connections and lookups but never cuts off an answer in progress.
- **Pricing:** with direct paths first, most people cost almost nothing, so a flat monthly price with a fair-use cap on relayed traffic is simpler than metering messages. A household covers the devices on one computer. Final prices wait for the cost test.
- **Desktop to desktop** (Toskar Pro away from home reaching the home computer) can't use mobile store billing; it's a later option with billing on toskar.ai.

## Testing and free access

Testers go through the real subscription flow without paying; nothing in the app or core gives a free pass that a client could claim.

- **Beta builds** (TestFlight, Play testing tracks): purchases use the stores' test environments, so testers subscribe for free, and App Review can try the feature. The rendezvous checks receipts with the production endpoints first, then the sandbox, and marks a test purchase's route token `sandbox`: it lasts as long as the store's test subscription (test renewals are sped up, which tests renewal and expiry too), has its own relay cap, and is left out of billing and cost figures. Only builds handed out for testing produce test purchases.
- **Specific people on the store build** (friends, press, early supporters): App Store offer codes, and Play promo codes or trials, give a real subscription for a while with no special code of ours.
- **Development without a store:** core and the apps take `TOSKAR_RELAY_URL`, so a developer runs the relay service locally with its own signing key, which production never trusts. A command on the service mints a route token for a route ID and an expiry, for testers who can't use a store build (such as Toskar Pro on a laptop); every grant is logged and can be revoked.
- **The free paths** (home network, a port forwarded by hand, Tailscale) need no subscription, so most of the feature can be tried without the service.

## Enterprise: your own tunnel, at scale

Organizations run their own way in instead of the subscription, and it has to work for hundreds of people. A separate guide ([remote-access-enterprise.md](remote-access-enterprise.md)) covers it; this is what it needs and what core and the apps must do for it.

**Or their own relay.** An organization that would rather keep its Toskar servers off the internet entirely runs the relay itself, licensed from Toskar, on its own hardware and name (toskar-relay's `docs/self-hosting.md`). Each Toskar server gets the relay's name and an enrollment secret under API Access → Your organization's relay, enrolls itself for a token, registers, and keeps its tunnel there; paired devices then find and reach it through that relay as they would through Toskar's. The enrollment secret is only ever sent to an organization's relay, never to Toskar's. `TOSKAR_RELAY_CA` adds a private CA for the relay's certificate.

**The shape:** Toskar runs on a server; people reach it at a hostname the organization controls, such as `toskar.example.com`, through its own tunnel or reverse proxy, with sign-in through its identity provider.

- **The way in,** any of:
  - a reverse proxy or load balancer on the organization's network or edge, such as nginx, Caddy, Traefik, or a cloud load balancer;
  - Cloudflare Tunnel with Cloudflare Access;
  - Tailscale or another mesh VPN with access rules, for phones enrolled in it;
  - the company VPN.
- **Sign-in:** OpenID Connect (`oidc`) for the web app, or a proxy that signs people in (`trusted_proxies`, `proxy_auth`), with groups mapped to Admin, Member, and Visitor. Profiles can be pinned to roles (#345), and portals (#205) serve people outside the company.
- **Phones, at scale:** each person pairs their own phone from their account in the web app (Connect a device), so an Admin doesn't pair hundreds by hand; removing a person or a device revokes its key. Phone requests carry that key in `Authorization`, so the proxy must let requests with a bearer token through to Toskar without its own login page (a bypass rule for the API, or service authentication), while browsers still sign in.
- **Certificates:** a certificate the phones already trust, from a public CA on the hostname, or the company's CA installed by MDM. *Needed in the apps:* when a server's certificate is trusted by the phone's system for its hostname, the app trusts it the usual way instead of pinning it, so renewals (every 90 days with Let's Encrypt) don't ask every phone to accept a changed certificate. Pinning stays for self-signed certificates, as now.
- **Capacity:** many people chatting at once need models on enough computers: pair GPU computers into the cluster (`docs/clustering.md`) and set placement, effort, and limits per profile. The guide gives sizing rules of thumb.
- **Operations:** the guide covers health checks for the load balancer (`GET /api/v1/health`), keeping the data directory on durable storage with backups, logs and the run-record retention, and updating Toskar.

## Threat model

| Threat | What stops it |
| --- | --- |
| The relay or rendezvous reads traffic | TLS is end to end to the computer; the relay never terminates it. |
| The relay or rendezvous sends the app to a fake server | The pin, which only pairing sets; a fake fails the TLS handshake. |
| A forged or stale address record | Signed with the pinned certificate's key and time-stamped; the app drops records that don't verify or are old. |
| Someone finds a person's computer by its fingerprint | Route IDs come from the route secret, never the fingerprint; records are encrypted. |
| A stranger reaches the remote listener | It answers only phone keys, on phone routes, with rate limits; no "this computer" or LAN trust, no control API. |
| A stolen phone | Removing the device in Settings revokes its key, on every path. |
| Someone takes over another person's relay route | Registering a route needs that route's token, which only its subscription produced. |
| The service goes away | Home network and do-it-yourself routes keep working; nothing depends on the service to work at home. |
| A router port left open | The mapping lapses on its own (it's renewed, not permanent) and is removed when the setting is turned off. |
| A free relay through a faked tester flag | There's no flag: only store-verified receipts, sandbox ones marked and capped, and grants the service signs. |

## Privacy

- **What the service keeps:** route IDs, encrypted address records, subscription status and expiry, relayed byte counts, and connection times, for billing and abuse limits. Never content.
- **Listings change** when this ships: toskar.ai/privacy/apps, the App Store privacy labels, and Play data safety ("encrypted in transit", "processed ephemerally"). Store text says "reach your computer from anywhere", without device brand names.

## Cost

- **Rendezvous:** one small instance, separate from the main cluster (which has no room); the push service (#460) can share it.
- **Relay:** chat is small. A question and its answer are a few kilobytes, streamed tokens included, so even a heavy user relays a few megabytes a month. Files and pictures dominate; the fair-use cap is in bytes for that reason.
- **The cost test** before prices: relay bandwidth and instance cost per thousand subscribers, and the share of people who need the relay at all.

## Building it

1. **This design.**
2. **Core:** the setting, the remote listener, port mapping (PCP, NAT-PMP, UPnP-IGD) written in Go without new dependencies, address discovery, and the status in Settings. Useful on its own, with a manual port forward.
3. **Core:** the route secret at pairing and on the LAN for paired devices, the signed and encrypted address record, registration, and the relay tunnel.
4. **`yeixio/toskar-relay`:** the rendezvous, receipt checks and route tokens, and the relay, with deployment on its own instance.
5. **Apps:** subscription screens (StoreKit 2, Play Billing), the receipt exchange, the "Use Toskar away from home" card, turning the setting on for an Admin, path selection (home, direct, relay) with the pin on each, the path shown, the offline and over-the-cap messages, and system trust for publicly trusted certificates.
6. **Store setup, privacy listings, docs:** the free routes, and the enterprise guide, beside the subscription; then the cost test and final prices.
