import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:gavya_bench/src/api/connect.dart';
import 'package:gavya_bench/src/api/ingestion.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

ConnectClient clientReturning(
  int status,
  Object body, {
  void Function(http.Request)? inspect,
}) {
  return ConnectClient(
    baseUrl: 'http://gateway.test',
    tenantId: 'tenant-1',
    httpClient: MockClient((req) async {
      inspect?.call(req);
      return http.Response(body is String ? body : jsonEncode(body), status,
          headers: {'content-type': 'application/json'});
    }),
  );
}

void main() {
  test('a procedure is addressed as Connect addresses it', () async {
    Uri? seen;
    final client = clientReturning(200, {'device': {}}, inspect: (r) => seen = r.url);
    await client.call('$ingestionService/RegisterDevice', {});

    expect(seen.toString(), 'http://gateway.test/ingestion.v1.IngestionService/RegisterDevice');
  });

  test('the tenant travels in the header the gateway forwards', () async {
    Map<String, String>? headers;
    final client = clientReturning(200, {}, inspect: (r) => headers = r.headers);
    await client.call('svc/Method', {});

    expect(headers?['X-Tenant-ID'], 'tenant-1');
  });

  test("the service's own code beats anything inferred from the status", () async {
    final client = clientReturning(400, {'code': 'already_exists', 'message': 'serial taken'});

    await expectLater(
      client.call('svc/Method', {}),
      throwsA(isA<ApiException>()
          .having((e) => e.code, 'code', ConnectCode.alreadyExists)
          .having((e) => e.message, 'message', 'serial taken')),
    );
  });

  test('a status with no usable body still yields a code the caller can act on', () async {
    final client = clientReturning(503, '<html>gateway down</html>');

    await expectLater(
      client.call('svc/Method', {}),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', ConnectCode.unavailable)),
    );
  });

  test('an unreachable service is retryable, and a rejected argument is not', () {
    expect(ApiException(ConnectCode.unavailable, '').retryable, isTrue);
    expect(ApiException(ConnectCode.resourceExhausted, '').retryable, isTrue);
    expect(ApiException(ConnectCode.aborted, '').retryable, isTrue);

    // Retrying these would either never succeed or risk repeating an effect.
    expect(ApiException(ConnectCode.invalidArgument, '').retryable, isFalse);
    expect(ApiException(ConnectCode.alreadyExists, '').retryable, isFalse);
    expect(ApiException(ConnectCode.internal, '').retryable, isFalse);
    expect(ApiException(ConnectCode.notFound, '').retryable, isFalse);
  });

  test('a network failure is reported as unavailable rather than as a crash', () async {
    final client = ConnectClient(
      baseUrl: 'http://gateway.test',
      tenantId: 't',
      httpClient: MockClient((_) async => throw const SocketFailure()),
    );

    await expectLater(
      client.call('svc/Method', {}),
      throwsA(isA<ApiException>()
          .having((e) => e.code, 'code', ConnectCode.unavailable)
          .having((e) => e.retryable, 'retryable', isTrue)),
    );
  });

  test('every outcome the service can return is understood', () {
    expect(DeliveryOutcome.parse('ACCEPTED'), DeliveryOutcome.accepted);
    expect(DeliveryOutcome.parse('DUPLICATE_REPLAY'), DeliveryOutcome.duplicateReplay);
    expect(DeliveryOutcome.parse('QUARANTINED'), DeliveryOutcome.quarantined);
    // An outcome added to the service later must not be mistaken for success.
    expect(DeliveryOutcome.parse('SOMETHING_NEW'), DeliveryOutcome.unrecognised);
    expect(DeliveryOutcome.unrecognised.isSettled, isFalse);
    expect(DeliveryOutcome.accepted.isSettled, isTrue);
    expect(DeliveryOutcome.duplicateReplay.isSettled, isTrue);
    expect(DeliveryOutcome.quarantined.isSettled, isFalse);
  });

  test('a delivery carries the payload as an object, not as a string of JSON', () async {
    Map<String, dynamic>? sent;
    final client = clientReturning(
      200,
      {'outcome': 'ACCEPTED', 'record_id': 'r1'},
      inspect: (r) => sent = jsonDecode(r.body) as Map<String, dynamic>,
    );

    await IngestionApi(client).deliver(DeliveryEnvelope(
      deviceId: 'dev',
      generation: 2,
      externalSessionId: 'sess',
      sequence: 9,
      payloadJson: '{"producer_ref":"P1"}',
      capturedAt: DateTime.utc(2026, 8, 22, 6, 30),
      actor: 'op',
    ));

    expect(sent?['payload'], {'producer_ref': 'P1'});
    expect(sent?['generation'], 2);
    expect(sent?['sequence'], 9);
    // The services parse RFC3339 and reject anything else.
    expect(sent?['captured_at'], '2026-08-22T06:30:00Z');
  });

  test('a batch reply is read back in the order it was sent', () async {
    final client = clientReturning(200, {
      'results': [
        {'outcome': 'ACCEPTED', 'record_id': 'r1'},
        {'outcome': 'QUARANTINED', 'quarantine_id': 'q1', 'reason': 'STALE_GENERATION'},
        {'outcome': 'DUPLICATE_REPLAY', 'record_id': 'r3'},
      ],
      'accepted': 1,
      'replayed': 1,
      'quarantined': 1,
    });

    final results = await IngestionApi(client).deliverBatch([
      for (var i = 1; i <= 3; i++)
        DeliveryEnvelope(
          deviceId: 'dev',
          generation: 1,
          externalSessionId: 'sess',
          sequence: i,
          payloadJson: '{}',
          capturedAt: DateTime.utc(2026, 8, 22),
          actor: 'op',
        )
    ]);

    expect(results.map((r) => r.outcome), [
      DeliveryOutcome.accepted,
      DeliveryOutcome.quarantined,
      DeliveryOutcome.duplicateReplay,
    ]);
    expect(results[1].reason, 'STALE_GENERATION');
  });
}

class SocketFailure implements Exception {
  const SocketFailure();
}
