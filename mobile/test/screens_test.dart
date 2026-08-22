import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:gavya_bench/src/capture/bench.dart';
import 'package:gavya_bench/src/capture/store.dart';
import 'package:gavya_bench/src/ui/app.dart';

import 'support.dart';

/// Every screen, actually built.
///
/// The logic tests below this file prove the bench cannot double-count milk;
/// none of them proves a screen renders at all. A null dereference in a build
/// method would have shipped, and an operator would meet it at a bench at five
/// in the morning.
///
/// The syncer is deliberately left off: these are about what is drawn, and a
/// retry loop outliving the test would leave a pending timer behind.

Future<void> pumpApp(WidgetTester tester, Bench bench) async {
  await tester.pumpWidget(BenchApp(bench: bench));
  await tester.pumpAndSettle();
}

/// Moves to a tab by its label, so the test breaks if the tab is renamed out
/// from under an operator who knows where things are.
Future<void> openTab(WidgetTester tester, String label) async {
  await tester.tap(find.widgetWithText(NavigationDestination, label));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('an unconfigured bench opens without crashing and says what it needs',
      (tester) async {
    final bench = benchOver(FakeGateway(), MemoryStore());
    await bench.bootstrap();

    await pumpApp(tester, bench);

    expect(find.text('Gavya bench'), findsOneWidget);
    // It must not offer a capture form to a bench that cannot deliver anything.
    expect(find.text('Record collection'), findsNothing);
    expect(find.textContaining('Provision this bench'), findsWidgets);
  });

  testWidgets('all three tabs build', (tester) async {
    final bench = await readyBench(FakeGateway(), MemoryStore());
    await pumpApp(tester, bench);

    await openTab(tester, 'Outbox');
    expect(find.textContaining('Nothing waiting'), findsOneWidget);

    await openTab(tester, 'Setup');
    expect(find.text('Where to send collections'), findsOneWidget);

    await openTab(tester, 'Collect');
    expect(find.text('Record collection'), findsOneWidget);
  });

  testWidgets('a ready bench offers the capture form and the next record number',
      (tester) async {
    final bench = await readyBench(FakeGateway(), MemoryStore());
    await pumpApp(tester, bench);

    expect(find.text('Producer'), findsOneWidget);
    expect(find.text('Litres'), findsOneWidget);
    expect(find.text('Fat %'), findsOneWidget);
    expect(find.text('SNF %'), findsOneWidget);
    expect(find.text('next #1'), findsOneWidget);
  });

  testWidgets('recording a collection clears the form and advances the number',
      (tester) async {
    final gw = FakeGateway();
    final bench = await readyBench(gw, MemoryStore());
    gw.handlers['$svc/DeliverRecord'] = (_) => {'outcome': 'ACCEPTED', 'record_id': 'r1'};
    await pumpApp(tester, bench);

    await tester.enterText(find.widgetWithText(TextField, 'Producer'), 'PRD-114');
    await tester.enterText(find.widgetWithText(TextField, 'Litres'), '12.5');
    await tester.enterText(find.widgetWithText(TextField, 'Fat %'), '4.1');
    await tester.enterText(find.widgetWithText(TextField, 'SNF %'), '8.6');
    await tester.tap(find.text('Record collection'));
    await tester.pumpAndSettle();

    expect(find.textContaining('Record 1 — PRD-114, 12.5 L'), findsOneWidget);
    expect(find.text('next #2'), findsOneWidget);
    expect(gw.delivered.single['payload'], containsPair('quantity_litres', '12.5'));
  });

  testWidgets('a quantity the columns cannot hold is refused before it is recorded',
      (tester) async {
    final bench = await readyBench(FakeGateway(), MemoryStore());
    await pumpApp(tester, bench);

    await tester.enterText(find.widgetWithText(TextField, 'Producer'), 'PRD-114');
    await tester.enterText(find.widgetWithText(TextField, 'Litres'), '12.3456');
    await tester.enterText(find.widgetWithText(TextField, 'Fat %'), '4.1');
    await tester.enterText(find.widgetWithText(TextField, 'SNF %'), '8.6');
    await tester.tap(find.text('Record collection'));
    await tester.pumpAndSettle();

    expect(find.textContaining('three decimal places'), findsOneWidget);
    expect(bench.outbox.entries, isEmpty, reason: 'a refused entry must not reach the outbox');
  });

  testWidgets('a collection with no producer is refused', (tester) async {
    final bench = await readyBench(FakeGateway(), MemoryStore());
    await pumpApp(tester, bench);

    await tester.enterText(find.widgetWithText(TextField, 'Litres'), '12.5');
    await tester.tap(find.text('Record collection'));
    await tester.pumpAndSettle();

    expect(find.text('Which producer is this?'), findsOneWidget);
    expect(bench.outbox.entries, isEmpty);
  });

  testWidgets('undelivered records are shown with their number and what is held',
      (tester) async {
    final gw = FakeGateway();
    final bench = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] = down;
    await captureOne(bench, 'PRD-114');
    await pumpApp(tester, bench);

    await openTab(tester, 'Outbox');

    expect(find.text('1 waiting to send'), findsOneWidget);
    expect(find.text('#1'), findsOneWidget);
    expect(find.textContaining('PRD-114'), findsOneWidget);
  });

  testWidgets('a held record shows the reason in a sentence an operator can act on',
      (tester) async {
    final gw = FakeGateway();
    final bench = await readyBench(gw, MemoryStore());
    gw.failures['$svc/DeliverRecord'] = down;
    await captureOne(bench, 'PRD-114');

    gw.failures.clear();
    gw.handlers['$svc/DeliverBatch'] = (_) => {
          'results': [
            {
              'outcome': 'QUARANTINED',
              'quarantine_id': 'q1',
              'reason': 'SEQUENCE_REGRESSION',
              'detail': 'sequence 1 follows 4',
            }
          ],
          'accepted': 0,
          'replayed': 0,
          'quarantined': 1,
        };
    await bench.sync();
    await pumpApp(tester, bench);
    await openTab(tester, 'Outbox');

    expect(find.text('Held for review'), findsOneWidget);
    expect(find.textContaining('a bench counting forward cannot do'), findsOneWidget);
  });

  testWidgets('a bench needing a generation cannot capture, and is told why', (tester) async {
    final gw = FakeGateway();
    gw.failures['$svc/RegisterDevice'] = (
      status: 409,
      body: {'code': 'already_exists', 'message': 'already registered'}
    );
    gw.handlers['$svc/ListDevices'] = (_) => {
          'devices': [device(generation: 3)['device']]
        };

    final bench = benchOver(gw, MemoryStore());
    await bench.bootstrap();
    await bench.saveConnection(gateway: 'http://gw.test', tenant: 't', operator: 'Ravi');
    await bench.provision(withSerial: 'BENCH-7', withKind: 'MOBILE_APP', withLabel: 'North dock');

    await pumpApp(tester, bench);

    expect(find.textContaining('needs a new generation'), findsOneWidget);
    expect(find.text('Record collection'), findsNothing);
  });

  testWidgets('the app renders in dark as well as light', (tester) async {
    final bench = await readyBench(FakeGateway(), MemoryStore());

    tester.platformDispatcher.platformBrightnessTestValue = Brightness.dark;
    addTearDown(tester.platformDispatcher.clearPlatformBrightnessTestValue);

    await pumpApp(tester, bench);

    expect(find.text('Record collection'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
