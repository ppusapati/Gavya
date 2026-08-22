import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:gavya_bench/src/api/connect.dart';
import 'package:gavya_bench/src/capture/bench.dart';
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


/// A service that is not answering — the bench's normal state between one patch
/// of signal and the next.
const down = (status: 503, body: {'code': 'unavailable', 'message': 'down'});

/// Accepts every record in a batch.
Map<String, Object> acceptAll(Map<String, dynamic> body) => {
      'results': [
        for (final _ in body['records'] as List) {'outcome': 'ACCEPTED', 'record_id': 'r'}
      ],
      'accepted': (body['records'] as List).length,
      'replayed': 0,
      'quarantined': 0,
    };

Future<void> captureOne(Bench b, String producer) => b.capture(collectionPayload(
      producerRef: producer,
      quantityLitres: '12.5',
      fatPercent: '4.1',
      snfPercent: '8.6',
    )).then((_) {});
