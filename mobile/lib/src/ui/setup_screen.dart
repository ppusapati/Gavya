import 'package:flutter/material.dart';

import '../capture/bench.dart';
import 'theme.dart';

/// Everything that has to be true before a single litre can be recorded: which
/// service to talk to, which bench this is, and which session is running.
class SetupScreen extends StatefulWidget {
  const SetupScreen({super.key, required this.bench});

  final Bench bench;

  @override
  State<SetupScreen> createState() => _SetupScreenState();
}

class _SetupScreenState extends State<SetupScreen> {
  late final _gateway = TextEditingController(text: widget.bench.gatewayUrl);
  late final _tenant = TextEditingController(text: widget.bench.tenantId);
  late final _operator = TextEditingController(text: widget.bench.operatorRef);
  late final _serial = TextEditingController(text: widget.bench.serial);
  late final _label = TextEditingController(text: widget.bench.label);
  late final _session = TextEditingController(text: widget.bench.externalSessionId);

  String _kind = 'MOBILE_APP';
  String _rollReason = 'APP_REINSTALL';

  @override
  void initState() {
    super.initState();
    _kind = widget.bench.kind;
  }

  @override
  void dispose() {
    for (final c in [_gateway, _tenant, _operator, _serial, _label, _session]) {
      c.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final b = widget.bench;

    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        _Section(
          title: 'Where to send collections',
          blurb: 'The gateway and the tenant whose collections this bench records.',
          children: [
            TextField(
              controller: _gateway,
              keyboardType: TextInputType.url,
              autocorrect: false,
              decoration: const InputDecoration(labelText: 'Gateway address'),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _tenant,
              autocorrect: false,
              decoration: const InputDecoration(labelText: 'Tenant id'),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _operator,
              decoration: const InputDecoration(
                labelText: 'Operator',
                helperText: 'Recorded against every collection this bench sends',
              ),
            ),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: b.busy
                  ? null
                  : () => b.saveConnection(
                        gateway: _gateway.text,
                        tenant: _tenant.text,
                        operator: _operator.text,
                      ),
              child: const Text('Save'),
            ),
          ],
        ),
        if (b.configured)
          _Section(
            title: 'This bench',
            blurb: 'The serial identifies this bench to the service. It is how the service '
                'recognises the same bench again after the app is reinstalled.',
            children: [
              TextField(
                controller: _serial,
                autocorrect: false,
                enabled: !b.provisioned,
                decoration: const InputDecoration(labelText: 'Serial'),
              ),
              const SizedBox(height: 12),
              DropdownButtonFormField<String>(
                initialValue: _kind,
                decoration: const InputDecoration(labelText: 'Kind'),
                items: [
                  for (final e in deviceKinds.entries)
                    DropdownMenuItem(value: e.key, child: Text(e.value)),
                ],
                onChanged: b.provisioned ? null : (v) => setState(() => _kind = v ?? _kind),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _label,
                decoration: const InputDecoration(
                  labelText: 'Label',
                  helperText: 'What people call this bench — “Ranga centre, north dock”',
                ),
              ),
              const SizedBox(height: 16),
              if (!b.provisioned)
                FilledButton(
                  onPressed: b.busy || _serial.text.trim().isEmpty
                      ? null
                      : () => b.provision(
                            withSerial: _serial.text,
                            withKind: _kind,
                            withLabel: _label.text,
                          ),
                  child: const Text('Provision this bench'),
                )
              else
                _Facts(facts: {
                  'Device': b.deviceId,
                  'Generation': b.generation.toString(),
                }),
            ],
          ),
        if (b.needsGenerationRoll)
          _Alert(
            tone: attentionColour(context),
            title: 'This bench needs a new generation',
            body: 'The service already knows this serial, but this install started its record '
                'numbering over. Until a new generation is opened, everything this bench sends '
                'would collide with what it sent before — and be held rather than counted.',
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                DropdownButtonFormField<String>(
                  initialValue: _rollReason,
                  decoration: const InputDecoration(labelText: 'Why the numbering restarted'),
                  items: [
                    for (final e in generationReasons.entries)
                      DropdownMenuItem(value: e.key, child: Text(e.value)),
                  ],
                  onChanged: (v) => setState(() => _rollReason = v ?? _rollReason),
                ),
                const SizedBox(height: 12),
                FilledButton(
                  onPressed: b.busy ? null : () => b.rollGeneration(_rollReason),
                  child: const Text('Open a new generation'),
                ),
              ],
            ),
          ),
        if (b.provisioned && !b.needsGenerationRoll)
          _Section(
            title: 'Session',
            blurb: 'A session is one shift at one bench. Records are numbered within it, and '
                'reopening the same session after losing signal continues where it left off.',
            children: [
              TextField(
                controller: _session,
                autocorrect: false,
                enabled: !b.sessionOpen,
                decoration: InputDecoration(
                  labelText: 'Session name',
                  suffixIcon: b.sessionOpen
                      ? null
                      : IconButton(
                          icon: const Icon(Icons.auto_awesome_outlined),
                          tooltip: 'Suggest one',
                          onPressed: () => setState(() {
                            _session.text = suggestedSessionId(b.serial, DateTime.now());
                          }),
                        ),
                ),
              ),
              const SizedBox(height: 16),
              if (!b.sessionOpen)
                FilledButton(
                  onPressed: b.busy || _session.text.trim().isEmpty
                      ? null
                      : () => b.openSession(_session.text),
                  child: const Text('Open session'),
                )
              else ...[
                _Facts(facts: {
                  'Session': b.externalSessionId,
                  'Generation': b.generation.toString(),
                  'Next record': b.outbox.nextSequence.toString(),
                }),
                const SizedBox(height: 12),
                OutlinedButton(
                  onPressed: b.busy ? null : b.closeSession,
                  child: const Text('Close session'),
                ),
                if (b.outbox.pending.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 8),
                    child: Text(
                      '${b.outbox.pending.length} records still to send. Sync before closing — '
                      'a closed session will not accept them.',
                      style: Theme.of(context)
                          .textTheme
                          .bodySmall
                          ?.copyWith(color: attentionColour(context)),
                    ),
                  ),
              ],
            ],
          ),
      ],
    );
  }
}

class _Section extends StatelessWidget {
  const _Section({required this.title, required this.blurb, required this.children});

  final String title;
  final String blurb;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(title, style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 4),
            Text(blurb, style: Theme.of(context).textTheme.bodySmall),
            const SizedBox(height: 16),
            ...children,
          ],
        ),
      ),
    );
  }
}

class _Alert extends StatelessWidget {
  const _Alert({required this.tone, required this.title, required this.body, required this.child});

  final Color tone;
  final String title;
  final String body;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Card(
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(6),
        side: BorderSide(color: tone, width: 1.5),
      ),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(title,
                style: Theme.of(context).textTheme.titleMedium?.copyWith(color: tone)),
            const SizedBox(height: 6),
            Text(body, style: Theme.of(context).textTheme.bodySmall),
            const SizedBox(height: 16),
            child,
          ],
        ),
      ),
    );
  }
}

class _Facts extends StatelessWidget {
  const _Facts({required this.facts});

  final Map<String, String> facts;

  @override
  Widget build(BuildContext context) {
    final mono = Theme.of(context).textTheme.bodySmall?.copyWith(fontFamily: 'monospace');
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final e in facts.entries)
          Padding(
            padding: const EdgeInsets.only(bottom: 4),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SizedBox(
                  width: 104,
                  child: Text(e.key, style: Theme.of(context).textTheme.bodySmall),
                ),
                Expanded(child: Text(e.value, style: mono)),
              ],
            ),
          ),
      ],
    );
  }
}
