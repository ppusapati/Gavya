import 'package:flutter/material.dart';

import '../capture/bench.dart';
import 'capture_screen.dart';
import 'outbox_screen.dart';
import 'setup_screen.dart';
import 'theme.dart';

class BenchApp extends StatelessWidget {
  const BenchApp({super.key, required this.bench});

  final Bench bench;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Gavya bench',
      theme: benchTheme(Brightness.light),
      darkTheme: benchTheme(Brightness.dark),
      home: BenchHome(bench: bench),
    );
  }
}

class BenchHome extends StatefulWidget {
  const BenchHome({super.key, required this.bench});

  final Bench bench;

  @override
  State<BenchHome> createState() => _BenchHomeState();
}

class _BenchHomeState extends State<BenchHome> {
  int _tab = 0;

  @override
  void initState() {
    super.initState();
    widget.bench.addListener(_onBenchChanged);
  }

  @override
  void dispose() {
    widget.bench.removeListener(_onBenchChanged);
    super.dispose();
  }

  /// Messages are shown once and then cleared, so a notice from one action does
  /// not linger over the next.
  void _onBenchChanged() {
    final b = widget.bench;
    final message = b.lastError ?? b.lastNotice;
    if (message == null || !mounted) return;
    final isError = b.lastError != null;
    b.clearMessages();
    ScaffoldMessenger.of(context)
      ..clearSnackBars()
      ..showSnackBar(SnackBar(
        content: Text(message),
        duration: Duration(seconds: isError ? 6 : 3),
        backgroundColor: isError ? Theme.of(context).colorScheme.error : null,
      ));
  }

  @override
  Widget build(BuildContext context) {
    final b = widget.bench;

    return ListenableBuilder(
      listenable: b,
      builder: (context, _) {
        final waiting = b.outbox.pending.length;

        return Scaffold(
          appBar: AppBar(
            title: Text(b.label.isEmpty ? 'Gavya bench' : b.label),
            bottom: b.busy
                ? const PreferredSize(
                    preferredSize: Size.fromHeight(2),
                    child: LinearProgressIndicator(minHeight: 2),
                  )
                : null,
            actions: [
              if (waiting > 0)
                Padding(
                  padding: const EdgeInsets.only(right: 12),
                  child: Center(
                    child: Text('$waiting waiting',
                        style: TextStyle(fontSize: 13, color: attentionColour(context))),
                  ),
                ),
            ],
          ),
          body: switch (_tab) {
            0 => CaptureScreen(bench: b),
            1 => OutboxScreen(bench: b),
            _ => SetupScreen(bench: b),
          },
          bottomNavigationBar: NavigationBar(
            selectedIndex: _tab,
            onDestinationSelected: (i) => setState(() => _tab = i),
            destinations: [
              const NavigationDestination(
                icon: Icon(Icons.water_drop_outlined),
                selectedIcon: Icon(Icons.water_drop),
                label: 'Collect',
              ),
              NavigationDestination(
                icon: Badge(
                  isLabelVisible: waiting > 0,
                  label: Text('$waiting'),
                  child: const Icon(Icons.outbox_outlined),
                ),
                selectedIcon: const Icon(Icons.outbox),
                label: 'Outbox',
              ),
              NavigationDestination(
                icon: Badge(
                  isLabelVisible: b.needsGenerationRoll,
                  child: const Icon(Icons.settings_outlined),
                ),
                selectedIcon: const Icon(Icons.settings),
                label: 'Setup',
              ),
            ],
          ),
        );
      },
    );
  }
}
