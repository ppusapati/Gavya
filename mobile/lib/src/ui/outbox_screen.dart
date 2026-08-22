import 'package:flutter/material.dart';

import '../capture/bench.dart';
import '../capture/outbox.dart';
import 'theme.dart';

/// What the bench is still holding, and what the service refused to count.
///
/// Nothing here can be lost by accident. A pending record has a number already
/// and can be sent as many times as the signal requires; a held record is on
/// the server, waiting for someone to work out what the device actually did.
class OutboxScreen extends StatelessWidget {
  const OutboxScreen({super.key, required this.bench});

  final Bench bench;

  @override
  Widget build(BuildContext context) {
    final pending = bench.outbox.pending;
    final held = bench.outbox.held;
    final stale = bench.outbox.stale(bench.generation);

    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Row(
          children: [
            Expanded(
              child: Text(
                pending.isEmpty ? 'Nothing waiting' : '${pending.length} waiting to send',
                style: Theme.of(context).textTheme.titleMedium,
              ),
            ),
            FilledButton.icon(
              style: FilledButton.styleFrom(minimumSize: const Size(140, 44)),
              onPressed: bench.busy || pending.isEmpty ? null : bench.sync,
              icon: const Icon(Icons.upload_outlined),
              label: const Text('Sync'),
            ),
          ],
        ),
        if (stale.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: 12),
            child: Text(
              '${stale.length} of these were recorded before this bench opened a new generation. '
              'They are sent exactly as captured — the service will hold them for review rather '
              'than count them, which is safer than renumbering a record that may already be in.',
              style: Theme.of(context)
                  .textTheme
                  .bodySmall
                  ?.copyWith(color: attentionColour(context)),
            ),
          ),
        const SizedBox(height: 16),
        for (final e in pending) _EntryCard(entry: e, currentGeneration: bench.generation),
        if (held.isNotEmpty) ...[
          const SizedBox(height: 8),
          Text('Held for review', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 4),
          Text(
            'These reached the service but were not counted as collections. They are kept there, '
            'not discarded — someone reviewing on the desk can see them and decide.',
            style: Theme.of(context).textTheme.bodySmall,
          ),
          const SizedBox(height: 12),
          for (final e in held)
            _EntryCard(
              entry: e,
              currentGeneration: bench.generation,
              onDismiss: () => bench.dismissHeld(e.localId),
            ),
        ],
        if (pending.isEmpty && held.isEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 48),
            child: Center(
              child: Text('Everything this bench recorded is on the server.',
                  style: Theme.of(context).textTheme.bodyMedium),
            ),
          ),
      ],
    );
  }
}

class _EntryCard extends StatelessWidget {
  const _EntryCard({required this.entry, required this.currentGeneration, this.onDismiss});

  final OutboxEntry entry;
  final int currentGeneration;
  final VoidCallback? onDismiss;

  @override
  Widget build(BuildContext context) {
    final payload = entry.payload;
    final held = entry.state == EntryState.held;
    final isStale = entry.state == EntryState.pending && entry.generation != currentGeneration;
    final mono = Theme.of(context).textTheme.bodySmall?.copyWith(fontFamily: 'monospace');

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Text('#${entry.sequence}', style: mono?.copyWith(fontWeight: FontWeight.w700)),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    '${payload['producer_ref'] ?? '—'} · ${payload['quantity_litres'] ?? '—'} L',
                    style: Theme.of(context).textTheme.bodyMedium,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (held)
                  _Tag(text: 'held', colour: attentionColour(context))
                else if (isStale)
                  _Tag(text: 'old generation', colour: attentionColour(context)),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              'fat ${payload['fat_percent'] ?? '—'} · snf ${payload['snf_percent'] ?? '—'} · '
              'gen ${entry.generation} · ${_time(entry.capturedAt)}',
              style: mono,
            ),
            if (held) ...[
              const SizedBox(height: 8),
              Text(
                _reasonSentence(entry.quarantineReason, entry.quarantineDetail),
                style: Theme.of(context).textTheme.bodySmall,
              ),
              if (onDismiss != null)
                Align(
                  alignment: Alignment.centerRight,
                  child: TextButton(
                    onPressed: onDismiss,
                    child: const Text('Remove from this list'),
                  ),
                ),
            ] else if (entry.lastError.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(
                '${entry.lastError} (${entry.attempts} ${entry.attempts == 1 ? 'attempt' : 'attempts'})',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ],
        ),
      ),
    );
  }

  static String _time(DateTime t) {
    final l = t.toLocal();
    return '${l.hour.toString().padLeft(2, '0')}:${l.minute.toString().padLeft(2, '0')}';
  }

  static String _reasonSentence(String reason, String detail) {
    switch (reason) {
      case 'TRANSPORT_IDENTITY_CONFLICT':
        return 'A different record already has this number in this session. $detail';
      case 'SEQUENCE_REGRESSION':
        return 'This number came after a higher one, which a bench counting forward cannot do. $detail';
      case 'UNTRUSTED_SESSION_IDENTITY':
        return 'The service has no record of this bench opening that session. $detail';
      case 'STALE_GENERATION':
        return 'This was recorded under a generation that has since been closed. $detail';
      case 'SESSION_NOT_ACCEPTING':
        return 'The session was already closed when this arrived. $detail';
      default:
        return detail.isEmpty ? 'Held for review.' : detail;
    }
  }
}

class _Tag extends StatelessWidget {
  const _Tag({required this.text, required this.colour});

  final String text;
  final Color colour;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        border: Border.all(color: colour),
        borderRadius: BorderRadius.circular(3),
      ),
      child: Text(text,
          style: TextStyle(fontSize: 11, color: colour, fontFamily: 'monospace')),
    );
  }
}
