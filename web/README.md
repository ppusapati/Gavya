# Gavya integrity workspaces

The desk-side half of the platform: the screens where a person decides what to do
about a difference the platform found. It is a browser client for the gateway —
every screen is a Connect unary JSON call the browser makes itself, so there is
no server here to render or to keep in step.

Three workspaces, reachable from one shell:

| Route | What it is for |
| --- | --- |
| `/` | The divergence mix over a period: how much money the two systems disagree about, and how much of that disagreement is already accounted for. |
| `/integrity`, `/integrity/[id]` | The queue of settlements where the incumbent's figure and the platform's independent recomputation differ, and the evidence behind each one. |
| `/mapping` | External identities: what an identifier from another system meant, at an instant. |
| `/mapping/conflicts` | Collection slots where two records both claim to be authoritative and the policy cannot rank them. |
| `/quarantine` | Records the ingestion service is holding rather than counting, because their transport identity could not be trusted. |
| `/balance`, `/balance/[id]` | Mass balance windows: what a route took in against what came out, and the reconciliation runs that distribute any imbalance across the flows in proportion to how well each is measured. |

## What the screens are careful about

- **A classification is deterministic.** It is decided from the evidence by rules,
  before any model is consulted. Where a model has offered an explanation it
  appears in a panel that says it is advisory and never next to the verdict — the
  same divergence would carry the same classification with the ML tier switched
  off.
- **Money is not summed across scales.** The overview groups by currency *and* by
  scale, because minor units at different scales are different units. Amounts are
  shown beside the currency they are in rather than totalled into one figure.
- **An error is not a stale answer.** When a query fails the previous result is
  discarded rather than left on screen under an error message; it was the answer
  to a different question.
- **Nothing is deleted.** Resolving a divergence, a slot conflict or a quarantined
  record adds a decision to its history. The record it was about stays.

## Running it

```sh
npm install
npm run dev          # http://localhost:5173
```

Open the workspace, set the gateway and sign in. The tenant you act for, what you
may do, and the name written into the audit trail of every resolution all come
from the session — none of them is something the browser states. The gateway
decides the tenant from the session and strips any tenant arriving with a
request, so a console that claimed one would be ignored at best.

The gateway and the session are remembered per browser; the password is not.

> This used to read "Phase-1 has no sign-in, so the tenant decides what is
> visible". That was true when the console was written and stopped being true
> when authorisation was added, and nobody came back here. In between, the
> console sent a tenant header the gateway does not read and no credential at
> all, so every screen in it answered 401. The check that now fails if either
> client stops sending a credential is in
> `services/gateway-service/handler/clients_auth_test.go`.

If the workspace is served from an origin other than the gateway's, the gateway
must be told to accept it:

```sh
CORS_ALLOWED_ORIGINS=http://localhost:5173 ./gateway-service
```

Origins are matched exactly, and no cross-origin call is answered unless one is
configured.

## Checks

```sh
npm run check        # types, across .ts and .svelte
npm run build        # static output in build/, index.html fallback
npm run smoke        # builds, then drives every route in a real browser
```

`npm run smoke` serves the built output against a stub gateway and walks each
route with Chromium, failing on any console error, page error, or missing content.
It catches what a type check cannot: a runtime rune mistake, a page that renders
its empty state because a field name does not match the wire. Set `CHROMIUM_PATH`
if the browser is installed somewhere Playwright does not look.

## Deploying

The build is static files with an `index.html` fallback, so it can be served from
anywhere — including the gateway itself, which is the arrangement that needs no
CORS configuration at all.
