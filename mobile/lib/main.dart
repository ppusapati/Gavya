import 'package:flutter/material.dart';

import 'src/capture/bench.dart';
import 'src/capture/store.dart';
import 'src/capture/syncer.dart';
import 'src/ui/app.dart';

/// The Gavya bench capture utility.
///
/// It records milk collections at a bench that is usually offline and always
/// interrupted. What makes it more than a form is the transport identity it
/// keeps: every record carries this device, its generation, its session and a
/// sequence allocated once and never reused, so a record delivered twice is
/// recognised as one record rather than counted as two.
Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  final store = await PreferencesStore.open();
  final bench = Bench(store);
  await bench.bootstrap();

  // The outbox drains on its own. Nothing about a bench's day guarantees anyone
  // will remember to press a button when the signal comes back.
  final syncer = Syncer(bench);
  bench.onBacklog = syncer.nudge;

  runApp(BenchApp(bench: bench, syncer: syncer));
}
