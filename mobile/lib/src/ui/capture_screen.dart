import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../capture/bench.dart';
import 'theme.dart';

/// The one screen that matters at the bench: a producer, a quantity, and two
/// quality figures. Everything else in this app exists to make what happens
/// here trustworthy.
class CaptureScreen extends StatefulWidget {
  const CaptureScreen({super.key, required this.bench});

  final Bench bench;

  @override
  State<CaptureScreen> createState() => _CaptureScreenState();
}

class _CaptureScreenState extends State<CaptureScreen> {
  final _producer = TextEditingController();
  final _litres = TextEditingController();
  final _fat = TextEditingController();
  final _snf = TextEditingController();
  final _note = TextEditingController();
  final _producerFocus = FocusNode();

  String? _error;
  String? _confirmation;

  @override
  void dispose() {
    for (final c in [_producer, _litres, _fat, _snf, _note]) {
      c.dispose();
    }
    _producerFocus.dispose();
    super.dispose();
  }

  /// Measurements stay as the operator typed them.
  ///
  /// This checks the shape of a decimal without converting it: parsing to a
  /// double here and re-printing it would silently change 3.05 into 3.0499…,
  /// and this number decides what a producer is paid.
  String? _checkDecimal(String raw, String field, {double? max}) {
    final v = raw.trim();
    if (v.isEmpty) return '$field is required';
    if (!RegExp(r'^\d{1,6}(\.\d{1,3})?$').hasMatch(v)) {
      return '$field must be a number, with up to three decimal places';
    }
    if (double.parse(v) <= 0) return '$field must be more than zero';
    if (max != null && double.parse(v) > max) return '$field looks wrong — over $max';
    return null;
  }

  Future<void> _record() async {
    final b = widget.bench;
    setState(() {
      _error = null;
      _confirmation = null;
    });

    if (_producer.text.trim().isEmpty) {
      setState(() => _error = 'Which producer is this?');
      return;
    }
    for (final check in [
      _checkDecimal(_litres.text, 'Quantity'),
      _checkDecimal(_fat.text, 'Fat', max: 15),
      _checkDecimal(_snf.text, 'SNF', max: 15),
    ]) {
      if (check != null) {
        setState(() => _error = check);
        return;
      }
    }

    final entry = await b.capture(collectionPayload(
      producerRef: _producer.text.trim(),
      quantityLitres: _litres.text.trim(),
      fatPercent: _fat.text.trim(),
      snfPercent: _snf.text.trim(),
      note: _note.text.trim(),
    ));

    if (!mounted) return;
    setState(() {
      _confirmation = 'Record ${entry.sequence} — ${_producer.text.trim()}, ${_litres.text.trim()} L';
      _producer.clear();
      _litres.clear();
      _fat.clear();
      _snf.clear();
      _note.clear();
    });
    _producerFocus.requestFocus();
  }

  @override
  Widget build(BuildContext context) {
    final b = widget.bench;

    if (!b.canCapture) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.pending_outlined, size: 40, color: attentionColour(context)),
              const SizedBox(height: 16),
              Text(
                b.needsGenerationRoll
                    ? 'This bench needs a new generation before it can record anything.'
                    : !b.provisioned
                        ? 'Provision this bench on the Setup tab first.'
                        : 'Open a session on the Setup tab to start collecting.',
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.bodyMedium,
              ),
            ],
          ),
        ),
      );
    }

    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Row(
          children: [
            Expanded(
              child: Text(
                b.externalSessionId,
                style: Theme.of(context).textTheme.titleMedium,
                overflow: TextOverflow.ellipsis,
              ),
            ),
            Text('next #${b.outbox.nextSequence}',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(fontFamily: 'monospace')),
          ],
        ),
        const SizedBox(height: 16),
        TextField(
          controller: _producer,
          focusNode: _producerFocus,
          autocorrect: false,
          textCapitalization: TextCapitalization.characters,
          decoration: const InputDecoration(labelText: 'Producer'),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _litres,
          keyboardType: const TextInputType.numberWithOptions(decimal: true),
          inputFormatters: [FilteringTextInputFormatter.allow(RegExp(r'[0-9.]'))],
          style: const TextStyle(fontSize: 28, fontWeight: FontWeight.w700),
          decoration: const InputDecoration(labelText: 'Litres'),
        ),
        const SizedBox(height: 12),
        Row(
          children: [
            Expanded(
              child: TextField(
                controller: _fat,
                keyboardType: const TextInputType.numberWithOptions(decimal: true),
                inputFormatters: [FilteringTextInputFormatter.allow(RegExp(r'[0-9.]'))],
                decoration: const InputDecoration(labelText: 'Fat %'),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: TextField(
                controller: _snf,
                keyboardType: const TextInputType.numberWithOptions(decimal: true),
                inputFormatters: [FilteringTextInputFormatter.allow(RegExp(r'[0-9.]'))],
                decoration: const InputDecoration(labelText: 'SNF %'),
              ),
            ),
          ],
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _note,
          decoration: const InputDecoration(labelText: 'Note (optional)'),
        ),
        const SizedBox(height: 20),
        FilledButton.icon(
          onPressed: b.busy ? null : _record,
          icon: const Icon(Icons.add),
          label: const Text('Record collection'),
        ),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.only(top: 12),
            child: Text(_error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error)),
          ),
        if (_confirmation != null)
          Padding(
            padding: const EdgeInsets.only(top: 12),
            child: Text(_confirmation!,
                style: TextStyle(color: Theme.of(context).colorScheme.primary)),
          ),
        const SizedBox(height: 20),
        Text(
          b.outbox.pending.isEmpty
              ? 'Everything recorded here is on the server.'
              : '${b.outbox.pending.length} records are held on this device until the next sync. '
                  'They are safe: each already has its number, and sending one twice cannot count it twice.',
          style: Theme.of(context).textTheme.bodySmall,
        ),
      ],
    );
  }
}
