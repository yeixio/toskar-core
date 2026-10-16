# Push to the Toskar apps

Notifications reach the Toskar app on a person's phones and tablets, even when the app is closed and the phone is away from home (#460). The watch shows what its phone gets. Only Toskar can send a push: Apple and Google take pushes signed with the app's own keys, which a self-hosted computer can't hold. So pushes go through Toskar's push relay, the same service as [access from anywhere](remote-access.md) (`yeixio/toskar-relay`). Push is free and needs no account or subscription.

## What the relay sees

- **The device's push token:** only inside the sealed handle, which only the relay can open. The computer never sees the token.
- **The computer's public key:** it signs every push.
- **Each push's content, by the destination's setting:**
  - **Title** (the default): the notification's title, which the person's own automation or Toskar wrote, such as "Back in stock: Trail runner, size 10". The text stays on the computer.
  - **Full:** the title and the text.
  - **Private:** only "You have a new Toskar notification".
- **The notification's ID and app path:** so the app opens it, and can fetch the rest from the computer.

The relay keeps no content and no tokens; it counts deliveries. Every push is listed in **What left this computer**, under the relay's address.

## Registering a device

1. The app asks for notification permission at a sensible moment, such as turning on "Notify me" on an automation, not at first launch.
2. The app asks its computer for the relay and the computer's key:
   - `GET /api/v1/me/push` returns `{relay, computer_key, registered, platform}`.
   - It's made with the device's own key.
3. The app registers its token with the relay:
   - `POST {relay}/v1/push/register` with `{token, platform: "ios"|"android", environment: "production"|"sandbox", app: <bundle ID>, computer_key}`.
   - The relay answers `{handle}`, a sealed, opaque string binding the token to that computer key.
4. The app gives the handle to its computer:
   - `PUT /api/v1/me/push` with `{handle, platform}`.
   - Only a paired device's key may; another key gets `PUSH_DEVICE_ONLY`.
   - A new handle replaces the old one, as when the token changes.
   - The first device to register adds a **Toskar app** destination if there's none, with the title-only default. Turning notifications on in the app is all it takes.

To stop: `DELETE /api/v1/me/push`. Disconnecting the device in Settings or API Access ends its pushes too, as its key no longer counts.

## Sending

A notification destination of kind `app` sends to every registered device of the notification's person.
- **Rules:** the usual categories, minimum severity, quiet hours, and digest apply.
- **Destination setting:** `app.content` is `title` (default), `full`, or `private`.
- **Delivery:** it counts as delivered when any device took it, so a retry doesn't repeat it on the others.
- **No devices:** if none is registered, the delivery fails with "no phone or tablet has notifications turned on in the Toskar app yet", and isn't retried.

Each push is `POST {relay}/v1/push`.

**Headers:**
- `Content-Type: application/json`
- `Toskar-Key`: this computer's Ed25519 public key, base64.
- `Toskar-Time`: Unix seconds.
- `Toskar-Signature`: base64 Ed25519 signature of `<Toskar-Time>\n<hex SHA-256 of the body>`.

**Body:**

```json
{
  "handle": "…",
  "title": "Back in stock: Trail runner, size 10",
  "body": "",
  "link": "/automations?id=…",
  "notification_id": "…",
  "category": "automation",
  "severity": "warning",
  "collapse": "<notification id>",
  "urgent": false
}
```

**The relay:**
1. Checks the signature, and that `Toskar-Time` is within 5 minutes.
2. Opens the handle, and refuses it unless the key sealed in it is `Toskar-Key`.
3. Rate-limits per computer.
4. Sends it on to Apple or Google:
   - `urgent` (the computer sets it for errors) asks for a time-sensitive interruption and high priority.
   - `collapse` replaces an earlier push of the same notification.

**Answers:**

| Status | Code | The computer |
| --- | --- | --- |
| 2xx | | counts it sent |
| 410 | `HANDLE_GONE` | forgets the device: the app was removed, or its token changed |
| 429 | `RATE_LIMITED` | tries again later |
| other 4xx | the relay's | fails the delivery for good |
| 5xx, network | | tries again later |

A computer can be pointed at another relay for testing with `TOSKAR_PUSH_RELAY`.
