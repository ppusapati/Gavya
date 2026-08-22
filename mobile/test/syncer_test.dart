import 'package:flutter_test/flutter_test.dart';
import 'package:gavya_bench/src/capture/store.dart';
import 'package:gavya_bench/src/capture/syncer.dart';

import 'support.dart';

/// The outbox has to empty itself. These assert that it does, and — just as
/// importantly — that it does not hammer a network that is not there.

void main() {
  test('an empty outbox costs nothing and does not count as a failure', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    final s = Syncer(b, sleep: (_) async {});

    expect(await s.attemptOnce(), isTrue);
    expect(s.consecutiveFailures, 0);
  });

  test('a bench that is not provisioned is idle rather than failing', () async {
    final gw = FakeGateway();
    final b = benchOver(gw, MemoryStore());
    await b.bootstrap();
    final s = Syncer(b, sleep: (_) async {});

    expect(await s.attemptOnce(), isTrue);
    expect(s.consecutiveFailures, 0,
        reason: 'backing off while the app sits unconfigured would delay the first real sync');
  });

  test('records captured with no signal are delivered once it returns', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] = down;
    await captureOne(b, 'P1');
    await captureOne(b, 'P2');
    expect(b.outbox.pending.length, 2);

    final s = Syncer(b, sleep: (_) async {});
    // Still nothing: the attempt fails and the records stay put.
    gw.failures['$svc/DeliverBatch'] = down;
    expect(await s.attemptOnce(), isFalse);
    expect(b.outbox.pending.length, 2);
    expect(s.consecutiveFailures, 1);

    // Signal returns. Nobody presses anything.
    gw.failures.clear();
    gw.handlers['$svc/DeliverBatch'] = acceptAll;
    expect(await s.attemptOnce(), isTrue);
    expect(b.outbox.pending, isEmpty);
    expect(s.consecutiveFailures, 0, reason: 'a success resets the backoff');
  });

  test('drain keeps trying until the outbox is empty', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] = down;
    await captureOne(b, 'P1');

    gw.failures['$svc/DeliverBatch'] = down;
    var slept = 0;
    final s = Syncer(b, sleep: (_) async {
      slept++;
      // The service comes back on the third wait, with no prompting.
      if (slept == 3) {
        gw.failures.clear();
        gw.handlers['$svc/DeliverBatch'] = acceptAll;
      }
    });

    await s.drain();

    expect(b.outbox.pending, isEmpty);
    expect(slept, 3);
    expect(s.isRunning, isFalse);
  });

  test('waits get longer as failures accumulate, and stop growing', () {
    final waits = [for (var i = 0; i < 12; i++) Syncer.defaultBackoff(i)];

    expect(waits.first, const Duration(seconds: 5));
    for (var i = 1; i < waits.length; i++) {
      expect(waits[i] >= waits[i - 1], isTrue,
          reason: 'wait $i (${waits[i]}) is shorter than wait ${i - 1} (${waits[i - 1]})');
    }
    // A whole morning out of range must not mean retrying every five seconds
    // for three hours; the battery has milk to weigh.
    expect(waits.last, const Duration(minutes: 5));
    expect(waits.every((w) => w <= const Duration(minutes: 5)), isTrue);
  });

  test('a nudge starts over, because coming back to the foreground is new evidence', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] = down;
    await captureOne(b, 'P1');

    gw.failures['$svc/DeliverBatch'] = down;
    final s = Syncer(b, sleep: (_) async {});
    await s.attemptOnce();
    await s.attemptOnce();
    expect(s.consecutiveFailures, 2);

    gw.failures.clear();
    gw.handlers['$svc/DeliverBatch'] = acceptAll;
    s.nudge();
    await Future<void>.delayed(Duration.zero);

    expect(s.consecutiveFailures, 0);
  });

  test('only one loop runs, so a resume does not race a retry already waiting', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] = down;
    await captureOne(b, 'P1');
    gw.failures['$svc/DeliverBatch'] = down;

    var attempts = 0;
    late final Syncer s;
    s = Syncer(b, sleep: (_) async {
      attempts++;
      if (attempts >= 2) s.stop();
    });

    await Future.wait([s.drain(), s.drain(), s.drain()]);

    expect(attempts, 2, reason: 'three drains ran three loops instead of one');
  });

  test('stopping ends the loop rather than leaving it retrying forever', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] = down;
    await captureOne(b, 'P1');
    gw.failures['$svc/DeliverBatch'] = down;

    final s = Syncer(b, sleep: (_) async {});
    s.stop();
    await s.drain();

    expect(s.isRunning, isFalse);
  });

  test('an automatic sync says nothing, so the app does not narrate the weather', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] = down;
    await captureOne(b, 'P1');
    b.clearMessages();

    gw.failures['$svc/DeliverBatch'] = down;
    await Syncer(b, sleep: (_) async {}).attemptOnce();

    expect(b.lastError, isNull, reason: 'losing signal is the normal state, not news');
    expect(b.lastNotice, isNull);
  });

  test('but a quarantined record still interrupts, because it is not being counted', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] = down;
    await captureOne(b, 'P1');
    b.clearMessages();

    gw.failures.clear();
    gw.handlers['$svc/DeliverBatch'] = (_) => {
          'results': [
            {'outcome': 'QUARANTINED', 'quarantine_id': 'q1', 'reason': 'STALE_GENERATION'}
          ],
          'accepted': 0,
          'replayed': 0,
          'quarantined': 1,
        };
    await Syncer(b, sleep: (_) async {}).attemptOnce();

    expect(b.lastNotice, contains('held for review'));
  });
}
