# TURN

Some listeners cannot reach the backend over UDP. Their network may block UDP, or it may be a strict NAT. A relay can
carry the stream for them.

The backend does not relay anything itself. It hands the page the credentials of a TURN server.

## How the credentials work

`GET /api/ice` returns the credentials in the form of the `iceServers` of the browser.

- They are made for each request from a secret that only the backend and the TURN server know.
- The username is the time at which the credentials stop working, and a random number.
- The password is the HMAC-SHA1 of the username. This is the shared-secret scheme of the TURN REST API, and coturn reads
  it with `use-auth-secret`.
- The secret never leaves the backend. It is read from a file and never from a command line.

The page fetches the credentials before it connects. If the call fails or takes longer than 3 seconds, the page goes on
without them. A listener with a direct path does not wait for a relay.

## Running it

`make stand-up-turn` starts the stand with coturn next to the backend. The coturn image is the official one, and it is
pinned. All capabilities are dropped except `NET_BIND_SERVICE`.

A real deployment is different. `compose.turn.yaml` describes the differences:

- coturn runs on its own host.
- It uses `turns:` on port 443, with a certificate.
- It denies relaying to private and loopback addresses.

The stand lets coturn relay to loopback only because it shares the network of the backend.

## What was tested

Automated:

- A Chromium that may only use relay candidates hears the stream. The test is `e2e/turn.spec.ts`, and it needs
  `STAND_URL`.

By hand, on the stand, and not repeated by a test:

- coturn refuses credentials with a wrong signature.
- coturn refuses credentials that are correct but expired.
- The relay worked over TCP (`transport=tcp`).

Not tried: the UDP address is also handed out, but it was not tried through a published UDP port.
