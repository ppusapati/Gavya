import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:gavya_bench/src/api/ingestion.dart';
import 'package:gavya_bench/src/capture/outbox.dart';
import 'package:gavya_bench/src/capture/store.dart';

/// These are the properties a bench depends on. Every one of them is about not
/// counting the same milk twice, or not losing it.

Future<Outbox> openOutbox([MemoryStore? store]) async {
  final o = Outbox(store ?? MemoryStore());
  await o.load();
  return o;
}

Future<OutboxEntry> capture(Outbox o, String producer, {int generation = 1}) => o.capture(
      deviceId: 'dev-1',
      generation: generation,
      externalSessionId: 'sess-1',
      payload: {'producer_ref': producer, 'quantity_litres': '12.5'},
    );

void main() {
  test('a sequence is allocated once and never handed out again', () async {
    final o = await openOutbox();
    final a = await capture(o, 'P1');
    final b = await capture(o, 'P2');
    final c = await capture(o, 'P3');

    expect([a.sequence, b.sequence, c.sequence], [1, 2, 3]);
    expect(o.nextSequence, 4);
  });

  test('a captured record survives the app being killed before it is sent', () async {
    final disk = MemoryStore();
    final first = await openOutbox(disk);
    await capture(first, 'P1');
    await capture(first, 'P2');

    // A second outbox over the same bytes is what the next launch sees.
    final afterRestart = Outbox(MemoryStore()..restore(disk.snapshot()));
    await afterRestart.load();

    expect(afterRestart.pending.length, 2);
    expect(afterRestart.pending.map((e) => e.sequence), [1, 2]);
    // And the counter continues rather than restarting, which would give the
    // next capture a sequence another record already owns.
    expect(afterRestart.nextSequence, 3);
  });

  test('a sequence is never reused even if only the counter was written', () async {
    final disk = MemoryStore();
    final o = await openOutbox(disk);
    await capture(o, 'P1');
    await capture(o, 'P2');

    // Simulate the entries write landing while the counter write is lost, by
    // rewinding the counter on disk. The outbox must trust the entries.
    final bytes = disk.snapshot();
    bytes['gavya.outbox.next_sequence.v1'] = '1';

    final recovered = Outbox(MemoryStore()..restore(bytes));
    await recovered.load();

    expect(recovered.nextSequence, 3, reason: 'a rewound counter would reissue sequence 1');
  });

  test('the payload sent on a retry is byte-identical to the one first captured', () async {
    final o = await openOutbox();
    final e = await capture(o, 'P1');

    final first = e.envelope('operator').payloadJson;
    final second = e.envelope('operator').payloadJson;

    expect(first, second);
    // The server hashes what it receives to recognise a replay, so a payload
    // that re-encoded differently would read as a second record claiming a slot
    // the first already holds, and be quarantined rather than recognised.
    expect(jsonDecode(first), {'producer_ref': 'P1', 'quantity_litres': '12.5'});
  });

  test('an accepted record leaves the outbox', () async {
    final o = await openOutbox();
    final e = await capture(o, 'P1');

    await o.settle(e.localId, DeliveryResult(outcome: DeliveryOutcome.accepted, recordId: 'r1'));

    expect(o.entries, isEmpty);
  });

  test('a redelivery the server already had also leaves the outbox', () async {
    final o = await openOutbox();
    final e = await capture(o, 'P1');

    await o.settle(
        e.localId, DeliveryResult(outcome: DeliveryOutcome.duplicateReplay, recordId: 'r1'));

    expect(o.entries, isEmpty, reason: 'a replay means the record is on the server');
  });

  test('a quarantined record is kept and never sent again', () async {
    final o = await openOutbox();
    final e = await capture(o, 'P1');

    await o.settle(
      e.localId,
      DeliveryResult(
        outcome: DeliveryOutcome.quarantined,
        quarantineId: 'q1',
        reason: 'SEQUENCE_REGRESSION',
        detail: 'sequence 1 follows 4',
      ),
    );

    expect(o.pending, isEmpty, reason: 'resending would not change the outcome');
    expect(o.held.length, 1);
    expect(o.held.single.quarantineReason, 'SEQUENCE_REGRESSION');
  });

  test('an outcome this app does not recognise never drops the record', () async {
    final o = await openOutbox();
    final e = await capture(o, 'P1');

    await o.settle(e.localId, DeliveryResult(outcome: DeliveryOutcome.unrecognised));

    expect(o.pending.length, 1);
    expect(o.pending.single.lastError, isNotEmpty);
  });

  test('a delivery that got no answer stays pending, unchanged', () async {
    final o = await openOutbox();
    final e = await capture(o, 'P1');
    final sequenceBefore = e.sequence;

    await o.noteFailure(e.localId, 'no signal');

    expect(o.pending.length, 1);
    expect(o.pending.single.sequence, sequenceBefore,
        reason: 'renumbering would make the retry a different record');
    expect(o.pending.single.attempts, 1);
  });

  test('rejoining a session takes the server high-water mark as a floor', () async {
    final o = await openOutbox();

    await o.alignTo(CaptureSession(
      id: 's',
      deviceId: 'dev-1',
      generation: 1,
      externalSessionId: 'sess-1',
      operatorRef: 'op',
      status: 'OPEN',
      lastSequence: 17,
      recordCount: 17,
    ));

    expect(o.nextSequence, 18);
  });

  test('rejoining never renumbers records this device has already allocated', () async {
    final o = await openOutbox();
    // Captured offline: the server has seen none of these.
    await capture(o, 'P1');
    await capture(o, 'P2');
    await capture(o, 'P3');

    // The server only knows about sequence 1, delivered before the signal went.
    await o.alignTo(CaptureSession(
      id: 's',
      deviceId: 'dev-1',
      generation: 1,
      externalSessionId: 'sess-1',
      operatorRef: 'op',
      status: 'OPEN',
      lastSequence: 1,
      recordCount: 1,
    ));

    expect(o.nextSequence, 4,
        reason: 'adopting the server number would hand 2 and 3 out a second time');
    expect(o.pending.map((e) => e.sequence), [1, 2, 3]);
  });

  test('a generation roll restarts numbering without restamping what was captured', () async {
    final o = await openOutbox();
    await capture(o, 'P1', generation: 1);
    await capture(o, 'P2', generation: 1);

    await o.onGenerationRolled();

    expect(o.nextSequence, 1, reason: 'a new generation is a fresh sequence space');
    // The captures keep the generation they were taken under. Restamping them
    // would give one collection two identities, and if the first had in fact
    // been admitted the same milk would be counted twice.
    expect(o.pending.map((e) => e.generation), [1, 1]);
    expect(o.pending.map((e) => e.sequence), [1, 2]);
    expect(o.stale(2).length, 2);
  });

  test('captures after a roll do not collide with the ones still queued', () async {
    final o = await openOutbox();
    await capture(o, 'P1', generation: 1);
    await o.onGenerationRolled();
    final after = await capture(o, 'P2', generation: 2);

    expect(after.sequence, 1);
    // Same number, different generation — which is precisely why generations
    // exist: they give each epoch its own sequence space.
    expect(o.pending.map((e) => '${e.generation}/${e.sequence}'), ['1/1', '2/1']);
  });

  test('dismissing a held record does not touch anything pending', () async {
    final o = await openOutbox();
    final a = await capture(o, 'P1');
    await capture(o, 'P2');
    await o.settle(a.localId, DeliveryResult(outcome: DeliveryOutcome.quarantined, reason: 'X'));

    await o.dismissHeld(a.localId);

    expect(o.held, isEmpty);
    expect(o.pending.length, 1);
  });

  test('dismiss refuses to remove a pending record', () async {
    final o = await openOutbox();
    final a = await capture(o, 'P1');

    await o.dismissHeld(a.localId);

    expect(o.pending.length, 1, reason: 'an undelivered record must not be discardable');
  });
}
