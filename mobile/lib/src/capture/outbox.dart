import 'dart:convert';

import '../api/ingestion.dart';
import 'store.dart';

/// Where an entry stands.
enum EntryState {
  /// Captured and durable, not yet known to the server.
  pending,

  /// The server is holding it rather than counting it. It stays on the device
  /// so the operator can see what happened, and it is never resent: a held
  /// record is already on the server, waiting for a person.
  held,
}

/// One capture, and everything needed to deliver it identically every time.
class OutboxEntry {
  OutboxEntry({
    required this.localId,
    required this.deviceId,
    required this.generation,
    required this.externalSessionId,
    required this.sequence,
    required this.payloadJson,
    required this.capturedAt,
    this.state = EntryState.pending,
    this.attempts = 0,
    this.lastError = '',
    this.quarantineId = '',
    this.quarantineReason = '',
    this.quarantineDetail = '',
  });

  final String localId;
  final String deviceId;
  final int generation;
  final String externalSessionId;

  /// Allocated once, at capture, and never changed. This is what makes a
  /// redelivery a redelivery: the same device, generation, session and
  /// sequence carrying the same payload is one record, however many times it
  /// is sent.
  final int sequence;

  /// The payload exactly as it was encoded at capture. Kept as text rather
  /// than as a map because the server identifies a replay by hashing what it
  /// receives, and a re-encoding that reordered a key would look like a
  /// different record claiming the same slot.
  final String payloadJson;

  final DateTime capturedAt;

  EntryState state;
  int attempts;
  String lastError;
  String quarantineId;
  String quarantineReason;
  String quarantineDetail;

  Map<String, dynamic> get payload => jsonDecode(payloadJson) as Map<String, dynamic>;

  DeliveryEnvelope envelope(String actor) => DeliveryEnvelope(
        deviceId: deviceId,
        generation: generation,
        externalSessionId: externalSessionId,
        sequence: sequence,
        payloadJson: payloadJson,
        capturedAt: capturedAt,
        actor: actor,
      );

  Map<String, dynamic> toJson() => {
        'local_id': localId,
        'device_id': deviceId,
        'generation': generation,
        'external_session_id': externalSessionId,
        'sequence': sequence,
        'payload_json': payloadJson,
        'captured_at': capturedAt.toUtc().toIso8601String(),
        'state': state.name,
        'attempts': attempts,
        'last_error': lastError,
        'quarantine_id': quarantineId,
        'quarantine_reason': quarantineReason,
        'quarantine_detail': quarantineDetail,
      };

  static OutboxEntry fromJson(Map<String, dynamic> j) => OutboxEntry(
        localId: j['local_id'] as String,
        deviceId: j['device_id'] as String,
        generation: (j['generation'] as num).toInt(),
        externalSessionId: j['external_session_id'] as String,
        sequence: (j['sequence'] as num).toInt(),
        payloadJson: j['payload_json'] as String,
        capturedAt: DateTime.parse(j['captured_at'] as String),
        state: j['state'] == 'held' ? EntryState.held : EntryState.pending,
        attempts: (j['attempts'] as num?)?.toInt() ?? 0,
        lastError: (j['last_error'] as String?) ?? '',
        quarantineId: (j['quarantine_id'] as String?) ?? '',
        quarantineReason: (j['quarantine_reason'] as String?) ?? '',
        quarantineDetail: (j['quarantine_detail'] as String?) ?? '',
      );
}

/// The bench's durable queue of captures.
///
/// Nothing here talks to the network. The outbox's job is narrower and more
/// important than that: to allocate each capture exactly one sequence, to make
/// it durable before anyone tries to send it, and to let go of it only once the
/// server has said it has it.
class Outbox {
  Outbox(this._store);

  static const _entriesKey = 'gavya.outbox.entries.v1';
  static const _counterKey = 'gavya.outbox.next_sequence.v1';

  final Store _store;

  final List<OutboxEntry> _entries = [];
  int _nextSequence = 1;
  int _counter = 0;

  List<OutboxEntry> get entries => List.unmodifiable(_entries);
  List<OutboxEntry> get pending =>
      _entries.where((e) => e.state == EntryState.pending).toList(growable: false);
  List<OutboxEntry> get held =>
      _entries.where((e) => e.state == EntryState.held).toList(growable: false);

  int get nextSequence => _nextSequence;

  Future<void> load() async {
    final raw = await _store.read(_entriesKey);
    _entries.clear();
    if (raw != null && raw.isNotEmpty) {
      final list = jsonDecode(raw) as List;
      for (final item in list) {
        _entries.add(OutboxEntry.fromJson(item as Map<String, dynamic>));
      }
    }
    _nextSequence = int.tryParse(await _store.read(_counterKey) ?? '') ?? 1;

    // The counter and the entries are written separately, so a process that
    // died between the two writes could leave a counter that is behind an
    // entry it already handed out. Trusting the lower of the two would hand the
    // same sequence to two different captures, so the higher one wins.
    for (final e in _entries) {
      if (e.sequence >= _nextSequence) _nextSequence = e.sequence + 1;
    }
  }

  Future<void> _persist() async {
    // The counter is written first. If only one of the two writes survives, a
    // counter that has run ahead of the entries costs a gap in the sequence,
    // which the server tolerates; the other order costs a reused sequence,
    // which it must not.
    await _store.write(_counterKey, _nextSequence.toString());
    await _store.write(_entriesKey, jsonEncode(_entries.map((e) => e.toJson()).toList()));
  }

  /// Aligns the counter with a session the server already knows about.
  ///
  /// The server's high-water mark is a floor, never an assignment: entries this
  /// device has allocated but not yet delivered sit above it, and adopting the
  /// server's number would hand their sequences out a second time.
  Future<void> alignTo(CaptureSession session) async {
    var floor = session.lastSequence + 1;
    for (final e in _entries) {
      if (e.externalSessionId == session.externalSessionId &&
          e.generation == session.generation &&
          e.sequence >= floor) {
        floor = e.sequence + 1;
      }
    }
    if (floor > _nextSequence) {
      _nextSequence = floor;
      await _persist();
    }
  }

  /// Records a capture and makes it durable.
  ///
  /// The entry exists on disk before this returns, so a crash between here and
  /// the first delivery attempt loses nothing and duplicates nothing.
  Future<OutboxEntry> capture({
    required String deviceId,
    required int generation,
    required String externalSessionId,
    required Map<String, dynamic> payload,
    DateTime? capturedAt,
  }) async {
    final entry = OutboxEntry(
      localId: _newLocalId(),
      deviceId: deviceId,
      generation: generation,
      externalSessionId: externalSessionId,
      sequence: _nextSequence,
      payloadJson: jsonEncode(payload),
      capturedAt: (capturedAt ?? DateTime.now()).toUtc(),
    );
    _nextSequence += 1;
    _entries.add(entry);
    await _persist();
    return entry;
  }

  /// Applies what the server said about one entry.
  ///
  /// Accepted and replayed both mean the record is on the server, so the entry
  /// goes. Quarantined means it is on the server too — being held rather than
  /// counted — so the entry stays visible but is never sent again.
  Future<void> settle(String localId, DeliveryResult result) async {
    final i = _entries.indexWhere((e) => e.localId == localId);
    if (i < 0) return;
    final e = _entries[i];
    e.attempts += 1;

    switch (result.outcome) {
      case DeliveryOutcome.accepted:
      case DeliveryOutcome.duplicateReplay:
        _entries.removeAt(i);
      case DeliveryOutcome.quarantined:
        e.state = EntryState.held;
        e.quarantineId = result.quarantineId;
        e.quarantineReason = result.reason;
        e.quarantineDetail = result.detail;
        e.lastError = '';
      case DeliveryOutcome.unrecognised:
        // An outcome this app does not know is not a reason to drop a record.
        e.lastError = 'the service returned an outcome this app does not recognise';
    }
    await _persist();
  }

  /// Notes a delivery that never got an answer. The entry stays pending: not
  /// knowing whether the server has it is exactly the case redelivery exists
  /// for, and sending it again is safe because its identity has not changed.
  Future<void> noteFailure(String localId, String reason) async {
    final e = _entries.where((e) => e.localId == localId).firstOrNull;
    if (e == null) return;
    e.attempts += 1;
    e.lastError = reason;
    await _persist();
  }

  /// Drops a held record from the device's view. It stays on the server, in
  /// quarantine, where the integrity workspace can see it.
  Future<void> dismissHeld(String localId) async {
    _entries.removeWhere((e) => e.localId == localId && e.state == EntryState.held);
    await _persist();
  }

  /// Starts a fresh sequence space after the device's generation was rolled.
  ///
  /// Entries captured under the old generation are kept exactly as they were.
  /// They are not restamped: restamping would give one capture two identities,
  /// and if the original had in fact been admitted the bench would have counted
  /// the same milk twice. Delivered as they stand, the server holds them in
  /// quarantine, which is where a decision about them belongs.
  Future<void> onGenerationRolled() async {
    _nextSequence = 1;
    for (final e in _entries) {
      if (e.state == EntryState.pending) {
        e.lastError = 'captured under generation ${e.generation}, which has since been closed';
      }
    }
    await _persist();
  }

  /// Entries that belong to a generation the device has moved past.
  List<OutboxEntry> stale(int currentGeneration) => _entries
      .where((e) => e.state == EntryState.pending && e.generation != currentGeneration)
      .toList(growable: false);

  String _newLocalId() {
    _counter += 1;
    // Local only — it never leaves the device and never identifies a record to
    // the server, which uses the device, generation, session and sequence.
    return '${DateTime.now().microsecondsSinceEpoch}-$_counter';
  }
}

extension _FirstOrNull<T> on Iterable<T> {
  T? get firstOrNull => isEmpty ? null : first;
}
