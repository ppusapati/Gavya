import 'dart:async';

import 'bench.dart';

/// Drains the outbox without being asked.
///
/// An offline queue that only empties when someone taps a button is not an
/// offline queue. The bench spends its morning in and out of signal, and the
/// operator's attention is on the milk: the app has to notice for itself that
/// it can reach the service again.
///
/// What it must not do is hammer a dead network. Attempts back off as failures
/// accumulate and reset the moment one succeeds, and the loop stops entirely
/// when there is nothing to send — so a bench sitting idle with an empty outbox
/// costs nothing.
class Syncer {
  Syncer(
    this._bench, {
    Duration Function(int consecutiveFailures)? backoff,
    Future<void> Function(Duration)? sleep,
  })  : _backoff = backoff ?? defaultBackoff,
        _sleep = sleep ?? _realSleep;

  static Future<void> _realSleep(Duration d) => Future<void>.delayed(d);

  /// Doubling from five seconds to five minutes.
  ///
  /// The first retries are quick because the common case is a few seconds of
  /// lost signal at a bench. The ceiling exists because the other common case
  /// is a whole morning out of range, and retrying every five seconds for three
  /// hours would flatten the battery the collection depends on.
  static Duration defaultBackoff(int consecutiveFailures) {
    const base = Duration(seconds: 5);
    const ceiling = Duration(minutes: 5);
    var d = base;
    for (var i = 0; i < consecutiveFailures && d < ceiling; i++) {
      d *= 2;
    }
    return d > ceiling ? ceiling : d;
  }

  final Bench _bench;
  final Duration Function(int) _backoff;
  final Future<void> Function(Duration) _sleep;

  int _failures = 0;
  bool _running = false;
  bool _stopped = false;

  /// How many attempts in a row have failed. Exposed so the loop's pacing can
  /// be asserted rather than inferred from timing.
  int get consecutiveFailures => _failures;

  bool get isRunning => _running;

  /// Tries once, now.
  ///
  /// Returns whether the outbox is empty afterwards. A bench that is not yet
  /// provisioned, or has nothing to send, counts as nothing to do rather than
  /// as a failure — otherwise the backoff would grow while the app sat idle.
  Future<bool> attemptOnce() async {
    if (!_bench.provisioned || _bench.outbox.pending.isEmpty) {
      _failures = 0;
      return true;
    }
    if (_bench.busy) return false;

    final ok = await _bench.sync(quiet: true);
    if (ok && _bench.outbox.pending.isEmpty) {
      _failures = 0;
      return true;
    }
    _failures++;
    return false;
  }

  /// Keeps trying until the outbox is empty or the syncer is stopped.
  ///
  /// Only one loop runs at a time, so a resume arriving while a retry is
  /// already waiting does not start a second one racing the first.
  Future<void> drain() async {
    if (_running || _stopped) return;
    _running = true;
    try {
      while (!_stopped) {
        if (await attemptOnce()) return;
        await _sleep(_backoff(_failures));
      }
    } finally {
      _running = false;
    }
  }

  /// Something changed that might mean the service is reachable — the app came
  /// back to the foreground, or a record was just captured.
  void nudge() {
    // A nudge is evidence, not proof, so the backoff starts over: the whole
    // point of coming back to the foreground is that the situation may be
    // different from the one that was failing.
    _failures = 0;
    unawaited(drain());
  }

  void stop() {
    _stopped = true;
  }
}
