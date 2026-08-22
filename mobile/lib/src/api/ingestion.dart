import 'dart:convert';

import 'connect.dart';

/// The ingestion service's procedures, as the bench uses them.
const ingestionService = 'ingestion.v1.IngestionService';

/// What the service decided about one delivered record.
///
/// This is a closed vocabulary and the app must handle all of it: two of the
/// three outcomes mean the record is safely on the server and may leave the
/// outbox, and the third means it is being held rather than counted.
enum DeliveryOutcome {
  /// A new record in a free slot.
  accepted,

  /// This exact record is already admitted. Redelivery after a lost reply is
  /// the normal cause, and it is not an error: the record is on the server.
  duplicateReplay,

  /// The record could not be admitted safely and is held for a person. It is
  /// not lost, and it is not counted as a collection.
  quarantined,

  /// The service returned something outside the vocabulary above. The record
  /// is kept and not treated as delivered.
  unrecognised;

  static DeliveryOutcome parse(String? wire) {
    switch (wire) {
      case 'ACCEPTED':
        return DeliveryOutcome.accepted;
      case 'DUPLICATE_REPLAY':
        return DeliveryOutcome.duplicateReplay;
      case 'QUARANTINED':
        return DeliveryOutcome.quarantined;
      default:
        return DeliveryOutcome.unrecognised;
    }
  }

  /// Whether the record is on the server and may leave the outbox.
  bool get isSettled => this == accepted || this == duplicateReplay;
}

class DeliveryResult {
  DeliveryResult({
    required this.outcome,
    this.recordId = '',
    this.quarantineId = '',
    this.reason = '',
    this.detail = '',
    this.conflictingRecordId = '',
  });

  final DeliveryOutcome outcome;
  final String recordId;
  final String quarantineId;
  final String reason;
  final String detail;
  final String conflictingRecordId;

  factory DeliveryResult.fromJson(Map<String, dynamic> j) => DeliveryResult(
        outcome: DeliveryOutcome.parse(j['outcome'] as String?),
        recordId: (j['record_id'] as String?) ?? '',
        quarantineId: (j['quarantine_id'] as String?) ?? '',
        reason: (j['reason'] as String?) ?? '',
        detail: (j['detail'] as String?) ?? '',
        conflictingRecordId: (j['conflicting_record_id'] as String?) ?? '',
      );
}

class Device {
  Device({
    required this.id,
    required this.serial,
    required this.kind,
    required this.label,
    required this.currentGeneration,
  });

  final String id;
  final String serial;
  final String kind;
  final String label;
  final int currentGeneration;

  factory Device.fromJson(Map<String, dynamic> j) => Device(
        id: (j['id'] as String?) ?? '',
        serial: (j['serial'] as String?) ?? '',
        kind: (j['kind'] as String?) ?? '',
        label: (j['label'] as String?) ?? '',
        currentGeneration: (j['current_generation'] as num?)?.toInt() ?? 0,
      );
}

class CaptureSession {
  CaptureSession({
    required this.id,
    required this.deviceId,
    required this.generation,
    required this.externalSessionId,
    required this.operatorRef,
    required this.status,
    required this.lastSequence,
    required this.recordCount,
  });

  final String id;
  final String deviceId;
  final int generation;
  final String externalSessionId;
  final String operatorRef;
  final String status;

  /// The highest sequence the server has admitted in this session. It is a
  /// floor for the device's own counter, never a replacement for it: records
  /// the device has allocated but not yet delivered sit above this.
  final int lastSequence;
  final int recordCount;

  bool get isOpen => status == 'OPEN';

  factory CaptureSession.fromJson(Map<String, dynamic> j) => CaptureSession(
        id: (j['id'] as String?) ?? '',
        deviceId: (j['device_id'] as String?) ?? '',
        generation: (j['generation'] as num?)?.toInt() ?? 0,
        externalSessionId: (j['external_session_id'] as String?) ?? '',
        operatorRef: (j['operator_ref'] as String?) ?? '',
        status: (j['status'] as String?) ?? '',
        lastSequence: (j['last_sequence'] as num?)?.toInt() ?? 0,
        recordCount: (j['record_count'] as num?)?.toInt() ?? 0,
      );
}

/// One record as the device will deliver it.
///
/// The payload travels as an already-encoded string rather than a map. The
/// server identifies a redelivery by hashing the payload it receives, so the
/// bytes must be identical on every attempt: re-encoding a map on each retry
/// would risk a different hash for the same capture, and the service would
/// read that as two different records claiming one slot.
class DeliveryEnvelope {
  DeliveryEnvelope({
    required this.deviceId,
    required this.generation,
    required this.externalSessionId,
    required this.sequence,
    required this.payloadJson,
    required this.capturedAt,
    required this.actor,
  });

  final String deviceId;
  final int generation;
  final String externalSessionId;
  final int sequence;
  final String payloadJson;
  final DateTime capturedAt;
  final String actor;

  Map<String, dynamic> toRequest(String tenantId) => {
        'tenant_id': tenantId,
        'device_id': deviceId,
        'generation': generation,
        'external_session_id': externalSessionId,
        'sequence': sequence,
        // Decoded here only so it is re-serialised as JSON rather than as a
        // string. The value round-trips unchanged.
        'payload': jsonDecode(payloadJson),
        'captured_at': capturedAt.toUtc().toIso8601String().replaceFirst(RegExp(r'\.\d+Z$'), 'Z'),
        'actor': actor,
      };
}

class IngestionApi {
  IngestionApi(this._client);

  final ConnectClient _client;

  String get tenantId => _client.tenantId;

  Future<Device> registerDevice({
    required String serial,
    required String kind,
    required String label,
    required String actor,
  }) async {
    final res = await _client.call('$ingestionService/RegisterDevice', {
      'tenant_id': tenantId,
      'serial': serial,
      'kind': kind,
      'label': label,
      'actor': actor,
    });
    return Device.fromJson(res['device'] as Map<String, dynamic>);
  }

  Future<List<Device>> listDevices({int limit = 200, int offset = 0}) async {
    final res = await _client.call('$ingestionService/ListDevices', {
      'tenant_id': tenantId,
      'limit': limit,
      'offset': offset,
    });
    final list = (res['devices'] as List?) ?? const [];
    return list.map((d) => Device.fromJson(d as Map<String, dynamic>)).toList();
  }

  Future<Device> getDevice(String id) async {
    final res = await _client.call('$ingestionService/GetDevice', {
      'id': id,
      'tenant_id': tenantId,
    });
    return Device.fromJson(res['device'] as Map<String, dynamic>);
  }

  /// Starts a new identity epoch for this device.
  ///
  /// This must happen whenever the device's sequence counter restarts — a
  /// reinstall, a wiped outbox, a restored backup. Without it the restarted
  /// sequences collide with ones already delivered and every record is
  /// quarantined instead of counted.
  Future<int> rollGeneration({required String deviceId, required String reason, required String actor}) async {
    final res = await _client.call('$ingestionService/RollGeneration', {
      'tenant_id': tenantId,
      'device_id': deviceId,
      'reason': reason,
      'actor': actor,
    });
    final gen = res['generation'] as Map<String, dynamic>;
    return (gen['generation'] as num?)?.toInt() ?? 0;
  }

  Future<CaptureSession> openSession({
    required String deviceId,
    required String externalSessionId,
    required String operatorRef,
    required String actor,
  }) async {
    final res = await _client.call('$ingestionService/OpenSession', {
      'tenant_id': tenantId,
      'device_id': deviceId,
      'external_session_id': externalSessionId,
      'operator_ref': operatorRef,
      'actor': actor,
    });
    return CaptureSession.fromJson(res['session'] as Map<String, dynamic>);
  }

  Future<CaptureSession> closeSession({required String sessionId, required String actor}) async {
    final res = await _client.call('$ingestionService/CloseSession', {
      'tenant_id': tenantId,
      'session_id': sessionId,
      'actor': actor,
    });
    return CaptureSession.fromJson(res['session'] as Map<String, dynamic>);
  }

  Future<DeliveryResult> deliver(DeliveryEnvelope e) async {
    final res = await _client.call('$ingestionService/DeliverRecord', e.toRequest(tenantId));
    return DeliveryResult.fromJson(res);
  }

  /// Delivers what the bench buffered while it was offline.
  ///
  /// The reply is one result per record in the order sent. One record's outcome
  /// never affects another's, so a single quarantined record in the middle of a
  /// morning's collections does not strand the rest.
  Future<List<DeliveryResult>> deliverBatch(List<DeliveryEnvelope> batch) async {
    final res = await _client.call('$ingestionService/DeliverBatch', {
      'records': batch.map((e) => e.toRequest(tenantId)).toList(),
    });
    final list = (res['results'] as List?) ?? const [];
    return list.map((r) => DeliveryResult.fromJson(r as Map<String, dynamic>)).toList();
  }
}
