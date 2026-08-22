import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:gavya_bench/src/api/connect.dart';
import 'package:gavya_bench/src/capture/bench.dart';
import 'package:gavya_bench/src/capture/outbox.dart';
import 'package:gavya_bench/src/capture/store.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A fake gateway that answers procedures by name, so a whole bench flow —
/// provision, roll, open, capture, sync — can be driven end to end.
class FakeGateway {
  FakeGateway();

  final List<String> calls = [];
  final List<Map<String, dynamic>> delivered = [];

  Map<String, Object Function(Map<String, dynamic>)> handlers = {};
  Map<String, ({int status, Object body})> failures = {};

  http.Client get client => MockClient((req) async {
        final procedure = req.url.path.substring(1);
        calls.add(procedure);
        final body = jsonDecode(req.body) as Map<String, dynamic>;

        // Recorded before any failure is simulated: `delivered` is what the
        // bench put on the wire, which is the thing under test. A reply that
        // never arrives does not unsend the record.
        if (procedure.endsWith('/DeliverRecord')) delivered.add(body);
        if (procedure.endsWith('/DeliverBatch')) {
          delivered.addAll((body['records'] as List).cast<Map<String, dynamic>>());
        }

        if (failures.containsKey(procedure)) {
          final f = failures[procedure]!;
          return http.Response(jsonEncode(f.body), f.status,
              headers: {'content-type': 'application/json'});
        }
        final handler = handlers[procedure];
        if (handler == null) {
          return http.Response(
              jsonEncode({'code': 'unimplemented', 'message': procedure}), 501,
              headers: {'content-type': 'application/json'});
        }
        return http.Response(jsonEncode(handler(body)), 200,
            headers: {'content-type': 'application/json'});
      });
}

Bench benchOver(FakeGateway gw, MemoryStore store) => Bench(
      store,
      clientFactory: (baseUrl, tenantId) =>
          ConnectClient(baseUrl: baseUrl, tenantId: tenantId, httpClient: gw.client),
    );

Map<String, Object> device({String id = 'dev-1', int generation = 1}) => {
      'device': {
        'id': id,
        'tenant_id': 't',
        'serial': 'BENCH-7',
        'kind': 'MOBILE_APP',
        'label': 'North dock',
        'current_generation': generation,
      }
    };

Map<String, Object> session({int generation = 1, int lastSequence = 0, int recordCount = 0}) => {
      'session': {
        'id': 'sess-row',
        'tenant_id': 't',
        'device_id': 'dev-1',
        'generation': generation,
        'external_session_id': 'BENCH-7-AM',
        'operator_ref': 'Ravi',
        'status': 'OPEN',
        'last_sequence': lastSequence,
        'record_count': recordCount,
      }
    };

const svc = 'ingestion.v1.IngestionService';

Future<Bench> readyBench(FakeGateway gw, MemoryStore store) async {
  gw.handlers = {
    '$svc/RegisterDevice': (_) => device(),
    '$svc/OpenSession': (_) => session(),
  };
  final b = benchOver(gw, store);
  await b.bootstrap();
  await b.saveConnection(gateway: 'http://gw.test', tenant: 't', operator: 'Ravi');
  await b.provision(withSerial: 'BENCH-7', withKind: 'MOBILE_APP', withLabel: 'North dock');
  await b.openSession('BENCH-7-AM');
  return b;
}

void main() {
  test('a bench cannot record anything until it is provisioned and a session is open', () async {
    final gw = FakeGateway();
    final b = benchOver(gw, MemoryStore());
    await b.bootstrap();

    expect(b.canCapture, isFalse);
    await b.saveConnection(gateway: 'http://gw.test', tenant: 't', operator: 'Ravi');
    expect(b.canCapture, isFalse, reason: 'no device yet');
  });

  test('a reinstalled bench adopts its device and is stopped until it rolls a generation',
      () async {
    final gw = FakeGateway();
    gw.failures['$svc/RegisterDevice'] = (
      status: 409,
      body: {'code': 'already_exists', 'message': 'a device with that serial is already registered'}
    );
    gw.handlers = {
      '$svc/ListDevices': (_) => {
            'devices': [device(generation: 3)['device']]
          },
      '$svc/RollGeneration': (_) => {
            'generation': {'id': 'g', 'device_id': 'dev-1', 'generation': 4, 'reason': 'APP_REINSTALL'}
          },
      '$svc/OpenSession': (_) => session(generation: 4),
    };

    final b = benchOver(gw, MemoryStore());
    await b.bootstrap();
    await b.saveConnection(gateway: 'http://gw.test', tenant: 't', operator: 'Ravi');
    await b.provision(withSerial: 'BENCH-7', withKind: 'MOBILE_APP', withLabel: 'North dock');

    expect(b.deviceId, 'dev-1');
    expect(b.needsGenerationRoll, isTrue);
    expect(b.canCapture, isFalse,
        reason: 'its counters restarted while the server still remembers the old ones');

    await b.rollGeneration('APP_REINSTALL');

    expect(b.generation, 4);
    expect(b.needsGenerationRoll, isFalse);
    // The session belonged to the old generation, so it is gone with it.
    expect(b.sessionOpen, isFalse);
  });

  test('a capture is durable before it is sent, and settles when the server has it', () async {
    final gw = FakeGateway();
    final store = MemoryStore();
    final b = await readyBench(gw, store);
    gw.handlers['$svc/DeliverRecord'] = (_) => {'outcome': 'ACCEPTED', 'record_id': 'r1'};

    await b.capture(collectionPayload(
      producerRef: 'P1',
      quantityLitres: '12.5',
      fatPercent: '4.1',
      snfPercent: '8.6',
    ));

    expect(b.outbox.entries, isEmpty, reason: 'the server confirmed it');
    expect(gw.delivered.single['sequence'], 1);
    expect(gw.delivered.single['payload'], containsPair('quantity_litres', '12.5'));
  });

  test('a capture with no signal is kept, and the same record is delivered later', () async {
    final gw = FakeGateway();
    final store = MemoryStore();
    final b = await readyBench(gw, store);
    gw.failures['$svc/DeliverRecord'] =
        (status: 503, body: {'code': 'unavailable', 'message': 'down'});

    await b.capture(collectionPayload(
        producerRef: 'P1', quantityLitres: '12.5', fatPercent: '4.1', snfPercent: '8.6'));
    await b.capture(collectionPayload(
        producerRef: 'P2', quantityLitres: '9.0', fatPercent: '3.9', snfPercent: '8.4'));

    expect(b.outbox.pending.length, 2);

    // The signal comes back.
    gw.failures.clear();
    gw.handlers['$svc/DeliverBatch'] = (body) => {
          'results': [
            for (final _ in body['records'] as List) {'outcome': 'ACCEPTED', 'record_id': 'r'}
          ],
          'accepted': (body['records'] as List).length,
          'replayed': 0,
          'quarantined': 0,
        };
    await b.sync();

    expect(b.outbox.pending, isEmpty);
    expect(b.lastError, isNull);
  });

  test('a record delivered twice keeps one identity across the two attempts', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());

    // First attempt: the record reaches the server, the reply does not reach us.
    gw.failures['$svc/DeliverRecord'] =
        (status: 503, body: {'code': 'unavailable', 'message': 'reply lost'});
    await b.capture(collectionPayload(
        producerRef: 'P1', quantityLitres: '12.5', fatPercent: '4.1', snfPercent: '8.6'));

    // Second attempt: the server recognises what it already has.
    gw.failures.clear();
    gw.handlers['$svc/DeliverBatch'] = (_) => {
          'results': [
            {'outcome': 'DUPLICATE_REPLAY', 'record_id': 'r1'}
          ],
          'accepted': 0,
          'replayed': 1,
          'quarantined': 0,
        };
    await b.sync();

    expect(b.outbox.entries, isEmpty);
    // Identical identity on both attempts is the whole mechanism: the server
    // could only recognise the replay because nothing about it changed.
    expect(gw.delivered.length, 2);
    expect(gw.delivered[0]['sequence'], gw.delivered[1]['sequence']);
    expect(gw.delivered[0]['generation'], gw.delivered[1]['generation']);
    expect(jsonEncode(gw.delivered[0]['payload']), jsonEncode(gw.delivered[1]['payload']));
  });

  test('a quarantined record is shown as held rather than reported as sent', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] =
        (status: 503, body: {'code': 'unavailable', 'message': 'down'});
    await b.capture(collectionPayload(
        producerRef: 'P1', quantityLitres: '12.5', fatPercent: '4.1', snfPercent: '8.6'));

    gw.failures.clear();
    gw.handlers['$svc/DeliverBatch'] = (_) => {
          'results': [
            {
              'outcome': 'QUARANTINED',
              'quarantine_id': 'q1',
              'reason': 'STALE_GENERATION',
              'detail': 'generation 1 has been closed',
            }
          ],
          'accepted': 0,
          'replayed': 0,
          'quarantined': 1,
        };
    await b.sync();

    expect(b.outbox.pending, isEmpty);
    expect(b.outbox.held.single.quarantineReason, 'STALE_GENERATION');
    expect(b.lastNotice, contains('held for review'));
  });

  test('closing a session with unsent records is refused', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] =
        (status: 503, body: {'code': 'unavailable', 'message': 'down'});
    await b.capture(collectionPayload(
        producerRef: 'P1', quantityLitres: '12.5', fatPercent: '4.1', snfPercent: '8.6'));

    await b.closeSession();

    expect(b.sessionOpen, isTrue, reason: 'closing first would strand the unsent records');
    expect(b.lastError, contains('have not been sent'));
    expect(gw.calls, isNot(contains('$svc/CloseSession')));
  });

  test('a bench that restarts mid-shift rejoins its session and keeps numbering', () async {
    final gw = FakeGateway();
    final disk = MemoryStore();
    final b = await readyBench(gw, disk);
    gw.failures['$svc/DeliverRecord'] =
        (status: 503, body: {'code': 'unavailable', 'message': 'down'});
    await b.capture(collectionPayload(
        producerRef: 'P1', quantityLitres: '12.5', fatPercent: '4.1', snfPercent: '8.6'));
    await b.capture(collectionPayload(
        producerRef: 'P2', quantityLitres: '8.0', fatPercent: '4.0', snfPercent: '8.5'));

    // The app is killed and relaunched over the same storage.
    final revived = benchOver(gw, MemoryStore()..restore(disk.snapshot()));
    await revived.bootstrap();

    expect(revived.deviceId, 'dev-1');
    expect(revived.sessionOpen, isTrue);
    expect(revived.outbox.pending.length, 2);

    // Rejoining is idempotent on the server and must not renumber anything.
    gw.failures.clear();
    gw.handlers['$svc/OpenSession'] = (_) => session(lastSequence: 0, recordCount: 0);
    await revived.openSession('BENCH-7-AM');

    expect(revived.outbox.nextSequence, 3);
    expect(revived.outbox.pending.map((e) => e.sequence), [1, 2]);
  });

  test('what a bench holds while offline is never lost to a failed sync', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] =
        (status: 503, body: {'code': 'unavailable', 'message': 'down'});
    for (final p in ['P1', 'P2', 'P3']) {
      await b.capture(collectionPayload(
          producerRef: p, quantityLitres: '5.0', fatPercent: '4.0', snfPercent: '8.5'));
    }

    gw.failures['$svc/DeliverBatch'] =
        (status: 503, body: {'code': 'unavailable', 'message': 'still down'});
    await b.sync();

    expect(b.outbox.pending.length, 3);
    expect(b.outbox.pending.every((e) => e.lastError.isNotEmpty), isTrue);
    expect(b.lastError, isNotNull);
  });

  test('a payload keeps decimals exactly as the operator typed them', () async {
    final gw = FakeGateway();
    final b = await readyBench(gw, MemoryStore());
    gw.handlers['$svc/DeliverRecord'] = (_) => {'outcome': 'ACCEPTED', 'record_id': 'r'};

    // 3.05 has no exact binary representation. Carried as text it stays 3.05;
    // carried as a double it becomes something a settlement would round wrong.
    await b.capture(collectionPayload(
        producerRef: 'P1', quantityLitres: '3.05', fatPercent: '4.15', snfPercent: '8.35'));

    final payload = gw.delivered.single['payload'] as Map<String, dynamic>;
    expect(payload['quantity_litres'], '3.05');
    expect(payload['fat_percent'], '4.15');
    expect(payload['snf_percent'], '8.35');
  });

  test('a suggested session name distinguishes bench and shift', () {
    final morning = suggestedSessionId('BENCH-7', DateTime(2026, 8, 22, 6));
    final evening = suggestedSessionId('BENCH-7', DateTime(2026, 8, 22, 18));

    expect(morning, 'BENCH-7-20260822-AM');
    expect(evening, 'BENCH-7-20260822-PM');
    expect(morning, isNot(evening));
  });

  test('an entry restored from storage delivers the payload it was captured with', () async {
    final disk = MemoryStore();
    final outbox = Outbox(disk);
    await outbox.load();
    await outbox.capture(
      deviceId: 'dev-1',
      generation: 1,
      externalSessionId: 'sess',
      payload: {'producer_ref': 'P1', 'quantity_litres': '3.05'},
    );
    final before = outbox.pending.single.envelope('op').payloadJson;

    final revived = Outbox(MemoryStore()..restore(disk.snapshot()));
    await revived.load();

    expect(revived.pending.single.envelope('op').payloadJson, before);
  });
}
