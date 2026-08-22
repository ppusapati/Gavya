# Gavya bench capture utility

The field half of the platform: the app an operator uses at a milk collection
bench. It records collections on a device that is usually offline and always
interrupted, and it delivers them in a way that cannot cause the same milk to be
counted twice.

## Why it is more than a form

Every record carries a transport identity — this device, its generation, its
session, and a sequence — and the ingestion service admits a record into that
slot exactly once. The app's job is to make that identity trustworthy from its
end:

- **A sequence is allocated once, at capture, and never reused.** It is written
  to disk before any attempt is made to send it, so a crash between capture and
  delivery loses nothing and duplicates nothing.
- **The payload sent on a retry is byte-identical to the one first captured.**
  The service recognises a redelivery by hashing what it receives; a payload that
  re-encoded differently would read as a second record claiming a slot the first
  already holds, and be quarantined instead of recognised.
- **A reply that never arrives is not a failure.** The record stays pending with
  its identity unchanged, and sending it again is the same record — which is what
  makes an unreliable link safe.
- **A generation roll starts a fresh sequence space without restamping what was
  already captured.** Restamping would give one collection two identities; if the
  first had in fact been admitted, the bench would have counted the same milk
  twice. Records from the closed generation are delivered as they stand and the
  service holds them for review.
- **Measurements travel as decimal strings.** A double would put a binary
  rounding error into a number that decides what a producer is paid, and nothing
  downstream can take it back out.
- **Nothing is discardable by accident.** An undelivered record cannot be removed
  from the outbox; only a record the service has confirmed it holds can be
  dismissed from the device's view.

## The reinstall case

Reinstalling the app clears its counters while the service still remembers the
old ones. The app detects this — the serial is already registered, so it adopts
the existing device — and then refuses to record anything until a new generation
is opened, with a reason. Without that, every record it sent would collide with
what it sent before and be held rather than counted.

## It syncs by itself

An offline queue that only empties when someone taps a button is not an offline
queue. The app retries on its own whenever there is something waiting: on
capture, on returning to the foreground, and on a backoff that doubles from five
seconds to five minutes and resets the moment an attempt succeeds. It stops
entirely when the outbox is empty, so an idle bench costs nothing.

Automatic attempts stay quiet. Losing signal at a bench is the normal state, not
news, and an app that announced every reconnection would train an operator to
ignore it. The one exception is a record the service quarantined — that is
something the bench recorded which is not being counted, and it interrupts.

Returning to the foreground also re-checks the session. A bench that restarts
believes whatever is on its disk; if the session had been closed meanwhile it
would keep collecting into one that refuses records, and nobody would find out
until the next sync.

## Screens

| Tab | |
| --- | --- |
| Collect | Producer, litres, fat, SNF. One record per entry, numbered as it is taken. |
| Outbox | What is still to send, and what the service is holding for review, with the reason in a sentence. |
| Setup | Gateway and tenant, this bench's identity, generation, and the open session. |

## Running it

```sh
flutter pub get
flutter run              # against a device or emulator
```

Point it at a gateway, give it a tenant and an operator name, provision the
bench with a serial, and open a session. Phase-1 has no sign-in: the tenant
decides which records this bench belongs to, and the operator name is recorded
against every collection it sends.

## Checks

```sh
flutter analyze
flutter test
```

The tests drive the whole bench flow against a fake gateway — provision, adopt,
roll, open, capture offline, sync, quarantine, restart mid-shift — and assert the
properties above rather than the widgets. They need no device.
