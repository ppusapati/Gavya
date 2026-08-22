import 'dart:convert';

import 'package:http/http.dart' as http;

/// The Connect unary JSON protocol, as the bench app speaks it.
///
/// A procedure is a POST to `/<fully.qualified.Service>/<Method>` with a JSON
/// body and a JSON reply. Failures arrive as `{code, message}` with an HTTP
/// status the code maps onto. Getting that code back to the caller is what
/// lets the outbox decide whether a record may be sent again — which, for a
/// device holding a day's collections, is the difference between a retry and a
/// double count.

/// The subset of Connect codes this app distinguishes.
enum ConnectCode {
  canceled,
  unknown,
  invalidArgument,
  deadlineExceeded,
  notFound,
  alreadyExists,
  permissionDenied,
  resourceExhausted,
  failedPrecondition,
  aborted,
  outOfRange,
  unimplemented,
  internal,
  unavailable,
  dataLoss,
  unauthenticated;

  static ConnectCode parse(String? wire) {
    switch (wire) {
      case 'canceled':
        return ConnectCode.canceled;
      case 'invalid_argument':
        return ConnectCode.invalidArgument;
      case 'deadline_exceeded':
        return ConnectCode.deadlineExceeded;
      case 'not_found':
        return ConnectCode.notFound;
      case 'already_exists':
        return ConnectCode.alreadyExists;
      case 'permission_denied':
        return ConnectCode.permissionDenied;
      case 'resource_exhausted':
        return ConnectCode.resourceExhausted;
      case 'failed_precondition':
        return ConnectCode.failedPrecondition;
      case 'aborted':
        return ConnectCode.aborted;
      case 'out_of_range':
        return ConnectCode.outOfRange;
      case 'unimplemented':
        return ConnectCode.unimplemented;
      case 'internal':
        return ConnectCode.internal;
      case 'unavailable':
        return ConnectCode.unavailable;
      case 'data_loss':
        return ConnectCode.dataLoss;
      case 'unauthenticated':
        return ConnectCode.unauthenticated;
      default:
        return ConnectCode.unknown;
    }
  }

  static ConnectCode fromStatus(int status) {
    switch (status) {
      case 400:
        return ConnectCode.invalidArgument;
      case 401:
        return ConnectCode.unauthenticated;
      case 403:
        return ConnectCode.permissionDenied;
      case 404:
        return ConnectCode.notFound;
      case 409:
        return ConnectCode.alreadyExists;
      case 412:
        return ConnectCode.failedPrecondition;
      case 429:
        return ConnectCode.resourceExhausted;
      case 501:
        return ConnectCode.unimplemented;
      case 503:
        return ConnectCode.unavailable;
      case 504:
        return ConnectCode.deadlineExceeded;
      default:
        return ConnectCode.internal;
    }
  }
}

class ApiException implements Exception {
  ApiException(this.code, this.message, {this.status = 0, this.procedure = ''});

  final ConnectCode code;
  final String message;
  final int status;
  final String procedure;

  /// Whether sending the same call again could plausibly succeed.
  ///
  /// Only transient conditions qualify. This is deliberately narrow: a delivery
  /// that is retried under a condition that was not transient is a record the
  /// bench may end up sending twice under two different identities.
  bool get retryable =>
      code == ConnectCode.unavailable ||
      code == ConnectCode.resourceExhausted ||
      code == ConnectCode.aborted ||
      code == ConnectCode.deadlineExceeded;

  /// What to put in front of an operator standing at a collection bench.
  String get humane {
    switch (code) {
      case ConnectCode.unavailable:
        return 'No answer from the collection service. Nothing was sent; it stays in the outbox.';
      case ConnectCode.deadlineExceeded:
        return 'The collection service took too long to answer. The record stays in the outbox.';
      case ConnectCode.notFound:
        return 'The service does not know this device or session.';
      case ConnectCode.alreadyExists:
        return message;
      default:
        return message.isEmpty ? 'The service reported an unexpected failure.' : message;
    }
  }

  @override
  String toString() => 'ApiException(${code.name}: $message)';
}

/// A Connect client for one gateway and one tenant.
class ConnectClient {
  ConnectClient({
    required this.baseUrl,
    required this.tenantId,
    http.Client? httpClient,
    this.timeout = const Duration(seconds: 20),
  }) : _http = httpClient ?? http.Client();

  final String baseUrl;
  final String tenantId;
  final http.Client _http;
  final Duration timeout;

  Future<Map<String, dynamic>> call(String procedure, Map<String, dynamic> body) async {
    final url = Uri.parse('${baseUrl.replaceAll(RegExp(r'/+$'), '')}/$procedure');

    http.Response response;
    try {
      response = await _http
          .post(
            url,
            headers: {
              'Content-Type': 'application/json',
              'Accept': 'application/json',
              'X-Tenant-ID': tenantId,
            },
            body: jsonEncode(body),
          )
          .timeout(timeout);
    } catch (cause) {
      // A bench loses its connection constantly. An unreachable service is not
      // an error the operator has to understand — it is the normal state
      // between syncs — so it is reported as retryable and nothing else.
      throw ApiException(
        ConnectCode.unavailable,
        'the collection service could not be reached',
        procedure: procedure,
      );
    }

    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw _toException(response, procedure);
    }

    final decoded = jsonDecode(response.body);
    if (decoded is! Map<String, dynamic>) {
      throw ApiException(
        ConnectCode.internal,
        'the service replied with something that is not a JSON object',
        status: response.statusCode,
        procedure: procedure,
      );
    }
    return decoded;
  }

  void close() => _http.close();

  ApiException _toException(http.Response response, String procedure) {
    final fallback = ConnectCode.fromStatus(response.statusCode);
    try {
      final body = jsonDecode(response.body);
      if (body is Map<String, dynamic>) {
        return ApiException(
          // The service's own code is more precise than anything inferred from
          // the status, so it wins whenever it is present.
          body['code'] is String ? ConnectCode.parse(body['code'] as String) : fallback,
          body['message'] is String ? body['message'] as String : response.reasonPhrase ?? '',
          status: response.statusCode,
          procedure: procedure,
        );
      }
    } catch (_) {
      // A body that is not JSON tells us nothing the status has not.
    }
    return ApiException(
      fallback,
      response.reasonPhrase ?? 'request failed',
      status: response.statusCode,
      procedure: procedure,
    );
  }
}
