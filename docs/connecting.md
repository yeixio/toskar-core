# Connecting to Toskar Core as a paired device

How an app or SDK finds a computer running Toskar Core, pairs with it, and reaches it afterwards, at home and from anywhere, the way the Toskar apps do (toskar-core#666). [remote-access.md](remote-access.md) explains why it works this way; this page says exactly what to do, so every SDK and app does the same thing.

[`api/connection-vectors.json`](../api/connection-vectors.json) has test vectors for the parts that must match byte for byte: route IDs, record keys, pins, and opening address records. They come from Core's reference code (`internal/rendezvous`), and its tests keep them in step. Every implementation should check itself against them.

The words *must*, *should*, and *may* mean what RFC 2119 says.

## The pieces

| Name | What it is | Where it comes from |
|---|---|---|
| **API key** | The device's own key, sent as `Authorization: Bearer <key>` | Pairing, once |
| **Pin** | `sha256:<hex>`: the lowercase hex SHA-256 of the computer's API certificate, in DER | Pairing (`tls_fingerprint`) |
| **Short pin** | The pin's first 8 hex digits, uppercase, as `ABCD-1234` | Shown on the computer and the device while pairing |
| **Route secret** | 32 random bytes, base64url without padding | Pairing, or `GET /api/v1/remote-access/route` at home |
| **Route ID** | Where the computer is registered: 32 lowercase base32 characters | Derived from the route secret |
| **Record key** | Opens the address record | Derived from the route secret |
| **Relay** | The relay the computer uses, such as `relay.toskar.ai` | Pairing (`relay`) |
| **Route token** | Lets a device look up the record on Toskar's relay, with a subscription | Pairing (`token`), or later from the computer |

Keep the API key, route secret, and route token secret, as a password is kept: in the platform's keychain where there is one. The pin isn't secret, but it must only ever come from pairing.

## 1. Finding a computer

Computers on the home network advertise themselves over mDNS (DNS-SD) as **`_localai._tcp`** in `local.`. The service's own port is for computers pairing with each other; the TXT record says where the API is:

| TXT key | Meaning |
|---|---|
| `name` | The computer's name, to show |
| `node_id` | Its node ID |
| `version` | Toskar Core's version |
| `api_port` | The API's port, usually 7331 |
| `api_lan` | `true` when the API answers on the network; `false` when it answers only on that computer, so say to turn on **Connect a device** there instead of "unreachable" |
| `pairing` | Whether it's pairing with another computer right now (not about devices) |

Offer the computers found, or let the person type an address. The API is at `<address>:<api_port>`. The same port answers both HTTPS and plain HTTP.

## 2. Pairing

1. The person opens **Connect a device** on the computer. It shows a 6-digit code, and the short pin.
2. The device connects to `https://<address>:<api_port>`, accepting whatever certificate it's shown for this connection only. It computes the pin of that certificate and shows the short pin, so the person can check it matches the computer's.
3. Over that connection, the device sends:

   ```http
   POST /api/v1/devices/pair
   Content-Type: application/json

   {"code": "123456", "device_name": "Kitchen tablet"}
   ```

   It needs no key, and answers only from the computer's local network.
4. The answer, `PairedDevice` in the API spec, is `201` with:
   - `api_key`;
   - `tls_fingerprint`, the pin;
   - `tls_short`;
   - `key`, the device's API key record;
   - with access from anywhere on, also `route_secret`, `route_id`, `relay`, and `token`.
5. The device **must** check that `tls_fingerprint` is the pin it computed in step 2. If it differs, something is in the middle: stop and pair again. Then save everything, with the pin stored for `<address>:<api_port>`.

| Answer | Meaning |
|---|---|
| `401 PAIRING_WRONG_CODE` | Not the code the computer shows. It may be tried again, but after 5 wrong codes the computer shows a new one |
| `410 PAIRING_EXPIRED` | The code expired: choose Connect a device on the computer again |
| `410 PAIRING_NOT_STARTED` | The computer isn't showing a code |
| `429 PAIRING_THROTTLED` | Too many tries from this address; wait a few minutes |
| `403 PAIRING_NOT_LOCAL` | The request didn't come from the computer's local network |

A device key reaches the routes a device needs, and only those (`auth.DeviceMayReach`): chat and conversations, files, the models (and installing one), automations to see and run, notifications, memory and personalization, adding knowledge, and privacy and reports. It doesn't reach Admin routes such as people, settings, or API Access. Scripts and servers that need those use a key from API Access instead.

## 3. The pin

Every connection after pairing, on every path, **must** be TLS, and the computer's certificate **must** have the saved pin:

- Compare the SHA-256 of the leaf certificate's DER with the pin. **Don't** check the certificate's names, dates, or signer: the pin is the trust.
- Check before sending anything, so the API key is never sent to a computer without the pin.
- On a mismatch, stop and tell the person the computer's certificate changed. **Never** fall back to plain HTTP, and never accept the new certificate on its own: pairing again is the only way to change a pin.

A computer from before HTTPS on the local network (toskar-core#405) answers only plain HTTP, and its pairing answer has no `tls_fingerprint`. An SDK **may** refuse it, and should say to update Toskar.

## 4. The route secret

A device paired before access from anywhere was turned on has no route secret. At home, it gets one from:

```http
GET /api/v1/remote-access/route
Authorization: Bearer <api key>
```

The answer, `DeviceRoute`, has `route_secret`, `route_id`, `relay`, and `token`. It answers only on the home network, never through the relay. Ask whenever a device at home has none, and update the token when it changes.

**Deriving** (the vectors' `routes`). The secret is base64url without padding. Then:

- **Route ID:** HKDF-SHA256 with the secret as the key material, an empty salt, and info `toskar route id`, for 20 bytes. Encode it as RFC 4648 base32, lowercase, without padding: 32 characters.
- **Record key:** HKDF-SHA256 with the same key material and empty salt, and info `toskar route key`, for 32 bytes.

## 5. Connecting

Try the ways in this order, and use the first that answers `GET /api/v1/health` with `{"status": "ok"}` over a pinned connection. Send the device's key with it (`Authorization: Bearer <api key>`): a computer that asks for keys answers health only with one, and the pin is checked before anything is sent. Check the pin on every connection, including a resumed TLS session; where a resumed session doesn't show the certificate, turn resumption off.

1. **Home:** the paired address, with 2.5 seconds to answer. Use this whenever it answers: it's free and fastest.
2. **Away:** the addresses in the computer's address record (below). Try the direct ones at once, and the relay's one second later, each with 6 seconds to answer. Keep the first that answers.

Remember which way worked, and try it first next time, but still prefer home whenever it answers. When the network changes, start again from home.

**Don't ask Toskar's relay without a route token.** For `relay.toskar.ai` with no token, don't look the record up at all: it only answers for a subscription, and nothing about the device or its computer should reach Toskar otherwise. An organization's own relay may answer without one.

### The address record

```http
GET https://<relay>/v1/routes/<route id>/record
Authorization: Bearer <route token>
```

The Authorization header is sent only when there's a token. A `200` answer's body is the sealed record, as raw bytes. Anything else means there's no record to use.

Opening it (the vectors' `records`):

1. **Decrypt.** The first 12 bytes are the nonce, and the rest is ciphertext with the 16-byte tag at its end. Decrypt with AES-256-GCM, the record key as key, and the route ID's ASCII as additional data. If that fails, refuse the record (`sealed`).
2. **Parse** the plaintext as JSON: `{"payload", "cert", "alg", "sig"}`. `payload`, `cert`, and `sig` are standard base64 with padding. If it doesn't parse, refuse the record (`sealed`).
3. **Check the certificate.** The SHA-256 of the `cert` bytes must be the pin (`pin`).
4. **Check the signature** over the `payload` bytes, with the certificate's public key (`pin` if it fails):
   - `ecdsa-sha256`: ECDSA over SHA-256, with an ASN.1 DER signature;
   - `rsa-pkcs1-sha256`: RSASSA-PKCS1-v1_5 with SHA-256;
   - `ed25519`: Ed25519 over the payload itself.

   The algorithm must match the key's type.
5. **Read the payload** as JSON: `{"v": 1, "route": "<route id>", "at": <unix seconds>, "addresses": ["host:port", …]}`. `v` must be 1 and `route` the route ID (`pin`).
6. **Check the time.** Refuse a record more than an hour old, or more than 5 minutes in the future (`stale`). The computer sends a new one every few minutes.

Each address is `host:port`, with IPv6 hosts in brackets. Reach it at `https://<address>`:

- An address whose host ends with `.<relay host>`, such as `<route id>.relay.toskar.ai:4443`, is the **relay**. Connect to that host and port, and send it as the TLS server name: the relay picks the computer by it and passes the encrypted bytes through without reading them.
- Every other address is **direct**: a port the computer opened on its router, an IPv6 address, or a port forwarded by hand.

On every way in, the pin applies as at home. A relay or record that lies can only make a connection fail the pin, never succeed with another computer.

Away from home, the computer answers only device keys, and only on the routes a device needs, on a separate port with stricter limits.

## Saying which way is in use

Name the path to the person: **At home**, **Direct**, or **Through Toskar's relay**, so a slow connection has an explanation. When nothing answers, say whether the record was missing (the computer may be off, asleep, or without access from anywhere) or whether its ways in failed.
