import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../api/connect.dart';
import '../api/ingestion.dart';
import 'outbox.dart';
import 'store.dart';

/// Device kinds the ingestion service recognises. A bench running this app is a
/// MOBILE_APP, and it says so: reinstalling resets its counters, which is
/// exactly what the generation mechanism exists to survive.
const deviceKinds = <String, String>{
  'MOBILE_APP': 'This app',
  'MILK_ANALYSER': 'Milk analyser',
  'PLATFORM_SCALE': 'Platform scale',
  'WEIGHBRIDGE': 'Weighbridge',
  'MANUAL_ENTRY': 'Manual entry',
};

/// Why a device's sequence counter restarted. The service will not accept a
/// roll without one, because a generation with no reason is an unexplained
/// discontinuity in a device's history.
const generationReasons = <String, String>{
  'APP_REINSTALL': 'The app was reinstalled or its data cleared',
  'FACTORY_RESET': 'The device was reset',
  'FIRMWARE_REFLASH': 'The device firmware was replaced',
  'CLOCK_RESET': "The device's clock was reset",
  'SUSPECTED_TAMPERING': 'The device may have been tampered with',
  'OPERATOR_REQUEST': 'An operator asked for a fresh start',
};

/// Everything the bench knows, and the only thing the screens talk to.
class Bench extends ChangeNotifier {
  Bench(this._store, {ConnectClient Function(String, String)? clientFactory})
      : _clientFactory = clientFactory ?? _defaultClient;


  static ConnectClient _defaultClient(String baseUrl, String tenantId) =>
      ConnectClient(baseUrl: baseUrl, tenantId: tenantId);

  static const _kGateway = 'gavya.gateway';
  static const _kTenant = 'gavya.tenant';
  static const _kOperator = 'gavya.operator';
  static const _kDeviceId = 'gavya.device.id';
  static const _kSerial = 'gavya.device.serial';
  static const _kKind = 'gavya.device.kind';
  static const _kLabel = 'gavya.device.label';
  static const _kGeneration = 'gavya.device.generation';
  static const _kSessionId = 'gavya.session.id';
  static const _kExternalSession = 'gavya.session.external';

  final Store _store;
  final ConnectClient Function(String, String) _clientFactory;

  late final Outbox outbox = Outbox(_store);

  String gatewayUrl = '';
  String tenantId = '';
  String operatorRef = '';

  String deviceId = '';
  String serial = '';
  String kind = 'MOBILE_APP';
  String label = '';
  int generation = 0;

  String sessionId = '';
  String externalSessionId = '';
  bool sessionOpen = false;

  /// Set when a device was adopted rather than freshly registered — a
  /// reinstall. Until the generation is rolled, this device's sequence counter
  /// has restarted while the server still remembers the old one, and every
  /// record it sends will be quarantined.
  bool needsGenerationRoll = false;

  bool busy = false;
  String? lastError;
  String? lastNotice;

  /// Called when a capture could not be delivered, so whatever is watching can
  /// start trying again. Kept as a callback rather than a dependency so the
  /// bench stays testable without a retry loop attached.
  void Function()? onBacklog;

  bool get configured => gatewayUrl.isNotEmpty && tenantId.isNotEmpty && operatorRef.isNotEmpty;
  bool get provisioned => configured && deviceId.isNotEmpty;
  bool get canCapture => provisioned && sessionOpen && !needsGenerationRoll;

  IngestionApi get _api => IngestionApi(_clientFactory(gatewayUrl, tenantId));

  Future<void> bootstrap() async {
    gatewayUrl = await _store.read(_kGateway) ?? '';
    tenantId = await _store.read(_kTenant) ?? '';
    operatorRef = await _store.read(_kOperator) ?? '';
    deviceId = await _store.read(_kDeviceId) ?? '';
    serial = await _store.read(_kSerial) ?? '';
    kind = await _store.read(_kKind) ?? 'MOBILE_APP';
    label = await _store.read(_kLabel) ?? '';
    generation = int.tryParse(await _store.read(_kGeneration) ?? '') ?? 0;
    sessionId = await _store.read(_kSessionId) ?? '';
    externalSessionId = await _store.read(_kExternalSession) ?? '';
    sessionOpen = sessionId.isNotEmpty;
    await outbox.load();
    notifyListeners();
  }

  Future<void> saveConnection({
    required String gateway,
    required String tenant,
    required String operator,
  }) async {
    gatewayUrl = gateway.trim();
    tenantId = tenant.trim();
    operatorRef = operator.trim();
    await _store.write(_kGateway, gatewayUrl);
    await _store.write(_kTenant, tenantId);
    await _store.write(_kOperator, operatorRef);
    notifyListeners();
  }

  /// Registers this bench, or adopts the one already registered under its
  /// serial.
  ///
  /// A reinstall lands in the second case: the serial is the same, the server
  /// still has the device, and this install's counters have started over. That
  /// is a generation roll, and it is flagged rather than done silently — the
  /// operator is told why their bench needs it.
  Future<void> provision({required String withSerial, required String withKind, required String withLabel}) async {
    await _guard(() async {
      serial = withSerial.trim();
      kind = withKind;
      label = withLabel.trim();

      Device device;
      try {
        device = await _api.registerDevice(
          serial: serial,
          kind: kind,
          label: label,
          actor: operatorRef,
        );
        lastNotice = 'Registered as a new bench.';
      } on ApiException catch (e) {
        if (e.code != ConnectCode.alreadyExists) rethrow;

        final existing = (await _api.listDevices()).where((d) => d.serial == serial).firstOrNull;
        if (existing == null) {
          throw ApiException(
            ConnectCode.notFound,
            'this serial is registered but the service did not return it — check the tenant',
          );
        }
        device = existing;
        needsGenerationRoll = true;
        lastNotice = 'This bench was already registered. Its counters have restarted here, '
            'so it needs a new generation before it can send anything.';
      }

      deviceId = device.id;
      generation = device.currentGeneration;
      await _store.write(_kDeviceId, deviceId);
      await _store.write(_kSerial, serial);
      await _store.write(_kKind, kind);
      await _store.write(_kLabel, label);
      await _store.write(_kGeneration, generation.toString());
    });
  }

  Future<void> rollGeneration(String reason) async {
    await _guard(() async {
      final next = await _api.rollGeneration(deviceId: deviceId, reason: reason, actor: operatorRef);
      generation = next;
      needsGenerationRoll = false;
      await _store.write(_kGeneration, generation.toString());
      await outbox.onGenerationRolled();

      // The old session belonged to the old generation, so it is not this
      // device's session any more.
      sessionId = '';
      externalSessionId = '';
      sessionOpen = false;
      await _store.remove(_kSessionId);
      await _store.remove(_kExternalSession);

      lastNotice = 'Now on generation $generation. Open a session to start collecting.';
    });
  }

  Future<void> openSession(String external) async {
    await _guard(() async {
      final session = await _api.openSession(
        deviceId: deviceId,
        externalSessionId: external.trim(),
        operatorRef: operatorRef,
        actor: operatorRef,
      );
      sessionId = session.id;
      externalSessionId = session.externalSessionId;
      sessionOpen = session.isOpen;
      generation = session.generation;

      await _store.write(_kSessionId, sessionId);
      await _store.write(_kExternalSession, externalSessionId);
      await _store.write(_kGeneration, generation.toString());

      // Reopening after a dropped connection returns the session that already
      // exists, so the counter is brought up to what the server has seen —
      // without ever moving it below what this device has already handed out.
      await outbox.alignTo(session);

      lastNotice = session.recordCount > 0
          ? 'Rejoined session ${session.externalSessionId}: ${session.recordCount} records already collected.'
          : 'Session ${session.externalSessionId} open.';
    });
  }

  /// Checks with the service that the session this bench thinks it is in is
  /// still open, and brings the sequence counter up to what the server has seen.
  ///
  /// Without this, a bench that restarts believes whatever was on its disk. If
  /// the session had been closed — by an operator on another device, or by the
  /// generation being rolled — it would keep taking collections into a session
  /// that refuses them, and nobody would find out until the next manual sync,
  /// possibly a morning's milk later.
  ///
  /// Reopening is idempotent and never reopens a closed session: the service
  /// returns the session that exists, and its status is what decides whether
  /// this bench may go on collecting.
  Future<void> rejoinSession() async {
    if (!provisioned || externalSessionId.isEmpty) return;

    final CaptureSession session;
    try {
      session = await _api.openSession(
        deviceId: deviceId,
        externalSessionId: externalSessionId,
        operatorRef: operatorRef,
        actor: operatorRef,
      );
    } on ApiException {
      // No answer is not evidence the session is gone. The bench keeps what it
      // knows and asks again later; its records are safe either way.
      return;
    }

    final wasOpen = sessionOpen;
    sessionId = session.id;
    sessionOpen = session.isOpen;
    generation = session.generation;
    await _store.write(_kSessionId, sessionId);
    await _store.write(_kGeneration, generation.toString());
    await outbox.alignTo(session);

    if (wasOpen && !sessionOpen) {
      // This one is worth interrupting for: the bench cannot collect, and an
      // operator who does not know that is about to lose their next hour.
      lastError = 'Session ${session.externalSessionId} has been closed. '
          'Open a new one before collecting anything else.';
    }
    notifyListeners();
  }

  Future<void> closeSession() async {
    await _guard(() async {
      if (outbox.pending.isNotEmpty) {
        throw ApiException(
          ConnectCode.failedPrecondition,
          '${outbox.pending.length} records have not been sent yet. Sync before closing, '
          'or they will be refused as belonging to a closed session.',
        );
      }
      await _api.closeSession(sessionId: sessionId, actor: operatorRef);
      sessionId = '';
      externalSessionId = '';
      sessionOpen = false;
      await _store.remove(_kSessionId);
      await _store.remove(_kExternalSession);
      lastNotice = 'Session closed.';
    });
  }

  /// Records one collection and tries to send it straight away.
  ///
  /// It is durable before the send is attempted, so losing the network — or the
  /// app — between the two costs nothing: the record is in the outbox with its
  /// sequence already allocated, and sending it later is the same record.
  Future<OutboxEntry> capture(Map<String, dynamic> payload) async {
    final entry = await outbox.capture(
      deviceId: deviceId,
      generation: generation,
      externalSessionId: externalSessionId,
      payload: payload,
    );
    notifyListeners();

    // A failure here is not the operator's problem: the record is safe and the
    // next sync will carry it.
    await _deliverOne(entry, quiet: true);
    if (outbox.pending.isNotEmpty) onBacklog?.call();
    return entry;
  }

  /// Delivers everything the bench is holding.
  ///
  /// A quiet sync is one nobody asked for — the app noticing it has signal, or
  /// retrying after a failure. It reports nothing on its own, because an
  /// operator with their hands in the milk does not need a message every time
  /// the network comes and goes. What it does still do is move the records.
  Future<bool> sync({bool quiet = false}) async {
    final queue = outbox.pending;
    if (queue.isEmpty) {
      if (!quiet) {
        lastNotice = 'Nothing to send.';
        notifyListeners();
      }
      return true;
    }

    var delivered = false;
    await _guard(quiet: quiet, () async {
      const chunk = 100;
      var accepted = 0, replayed = 0, quarantined = 0;

      for (var i = 0; i < queue.length; i += chunk) {
        final slice = queue.sublist(i, (i + chunk).clamp(0, queue.length));
        List<DeliveryResult> results;
        try {
          results = await _api.deliverBatch(slice.map((e) => e.envelope(operatorRef)).toList());
        } on ApiException catch (e) {
          for (final entry in slice) {
            await outbox.noteFailure(entry.localId, e.humane);
          }
          rethrow;
        }

        // The reply is one result per record, in the order sent. A short reply
        // would mean the tail was not answered for, so those entries stay
        // pending rather than being matched against someone else's result.
        for (var j = 0; j < slice.length && j < results.length; j++) {
          final r = results[j];
          await outbox.settle(slice[j].localId, r);
          switch (r.outcome) {
            case DeliveryOutcome.accepted:
              accepted++;
            case DeliveryOutcome.duplicateReplay:
              replayed++;
            case DeliveryOutcome.quarantined:
              quarantined++;
            case DeliveryOutcome.unrecognised:
              break;
          }
        }
      }

      delivered = true;
      final parts = <String>[
        if (accepted > 0) '$accepted sent',
        if (replayed > 0) '$replayed already had',
        if (quarantined > 0) '$quarantined held for review',
      ];
      final summary = parts.isEmpty ? 'Nothing was accepted.' : '${parts.join(', ')}.';
      // A quarantined record is the one outcome worth interrupting for, even
      // when nobody asked for this sync: it means something the bench recorded
      // is not being counted.
      if (!quiet || quarantined > 0) lastNotice = summary;
    });
    return delivered;
  }

  Future<void> dismissHeld(String localId) async {
    await outbox.dismissHeld(localId);
    notifyListeners();
  }

  Future<void> _deliverOne(OutboxEntry entry, {bool quiet = false}) async {
    try {
      final result = await _api.deliver(entry.envelope(operatorRef));
      await outbox.settle(entry.localId, result);
      if (result.outcome == DeliveryOutcome.quarantined && !quiet) {
        lastError = 'Held for review: ${result.detail}';
      }
    } on ApiException catch (e) {
      await outbox.noteFailure(entry.localId, e.humane);
      if (!quiet) lastError = e.humane;
    } finally {
      notifyListeners();
    }
  }

  Future<void> _guard(Future<void> Function() body, {bool quiet = false}) async {
    busy = true;
    lastError = null;
    lastNotice = null;
    notifyListeners();
    try {
      await body();
    } on ApiException catch (e) {
      // An unreachable service during a sync nobody asked for is the normal
      // state between one patch of signal and the next. Saying so every time
      // would train an operator to ignore the app's messages.
      if (!quiet) lastError = e.humane;
    } catch (e) {
      if (!quiet) lastError = e.toString();
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  void clearMessages() {
    lastError = null;
    lastNotice = null;
    notifyListeners();
  }
}

/// A default session name that reads like the shift it covers, so two benches
/// on the same day do not collide and an operator can recognise their own.
String suggestedSessionId(String serial, DateTime now) {
  final d =
      '${now.year}${now.month.toString().padLeft(2, '0')}${now.day.toString().padLeft(2, '0')}';
  final shift = now.hour < 12 ? 'AM' : 'PM';
  return '$serial-$d-$shift';
}

/// The payload of one collection, in the shape the platform expects.
Map<String, dynamic> collectionPayload({
  required String producerRef,
  required String quantityLitres,
  required String fatPercent,
  required String snfPercent,
  String note = '',
}) {
  // Measurements travel as decimal strings. A double would introduce a binary
  // rounding error into a number that decides what a producer is paid, and no
  // amount of care downstream can take it back out.
  return {
    'kind': 'MILK_COLLECTION',
    'producer_ref': producerRef,
    'quantity_litres': quantityLitres,
    'fat_percent': fatPercent,
    'snf_percent': snfPercent,
    if (note.isNotEmpty) 'note': note,
  };
}

String prettyPayload(String payloadJson) =>
    const JsonEncoder.withIndent('  ').convert(jsonDecode(payloadJson));

extension _FirstOrNull<T> on Iterable<T> {
  T? get firstOrNull => isEmpty ? null : first;
}
