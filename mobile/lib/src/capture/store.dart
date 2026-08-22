import 'package:shared_preferences/shared_preferences.dart';

/// Somewhere durable to keep the outbox and the bench's identity.
///
/// It is an interface because everything that matters about the outbox — that a
/// sequence is allocated once, that a record survives the app being killed
/// between capture and delivery — is a property of what was written, and those
/// properties should be testable without a device attached.
abstract class Store {
  Future<String?> read(String key);
  Future<void> write(String key, String value);
  Future<void> remove(String key);
}

class PreferencesStore implements Store {
  PreferencesStore(this._prefs);

  final SharedPreferences _prefs;

  static Future<PreferencesStore> open() async =>
      PreferencesStore(await SharedPreferences.getInstance());

  @override
  Future<String?> read(String key) async => _prefs.getString(key);

  @override
  Future<void> write(String key, String value) async {
    // Deliberately awaited by callers before anything is sent: a record that
    // reached the network but not the disk would be re-captured under a new
    // sequence after a crash, and counted twice.
    await _prefs.setString(key, value);
  }

  @override
  Future<void> remove(String key) async => _prefs.remove(key);
}

/// For tests, and for a platform where storage is unavailable.
class MemoryStore implements Store {
  final Map<String, String> _values = {};

  @override
  Future<String?> read(String key) async => _values[key];

  @override
  Future<void> write(String key, String value) async => _values[key] = value;

  @override
  Future<void> remove(String key) async => _values.remove(key);

  /// Everything written so far, so a test can build a second store over the
  /// same bytes and prove the outbox really survives a restart.
  Map<String, String> snapshot() => Map.of(_values);

  void restore(Map<String, String> from) => _values.addAll(from);
}
