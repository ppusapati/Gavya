// A smoke test over the built workspaces: serve the static output, drive a real
// browser through every route, and fail on any console error or page error.
// svelte-check cannot see a runtime rune mistake; this can.
import { chromium } from 'playwright';
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';

const ROOT = new URL('./build/', import.meta.url).pathname;
const TYPES = {
	'.html': 'text/html',
	'.js': 'text/javascript',
	'.css': 'text/css',
	'.json': 'application/json',
	'.svg': 'image/svg+xml',
	'.txt': 'text/plain'
};

// Stands in for the gateway. Every procedure the workspaces call answers with a
// shape the real services produce, so the pages render real data paths.
const GATEWAY = {
	'/shadowsettlement.v1.ShadowSettlementService/Summarise': {
		summaries: [
			{ classification: 'UNEXPLAINED', currency: 'INR', amount_scale: 2, count: 3, total_abs_minor_units: 45120 },
			{ classification: 'ROUNDING_DIFFERENCE', currency: 'INR', amount_scale: 2, count: 41, total_abs_minor_units: 41 },
			{ classification: 'MATCH', currency: 'INR', amount_scale: 2, count: 812, total_abs_minor_units: 0 }
		]
	},
	'/shadowsettlement.v1.ShadowSettlementService/ListDivergences': {
		divergences: [
			{
				id: '01JGAVYADIVERGENCE0000001A', tenant_id: 't', assertion_id: 'a1', computation_id: 'c1',
				producer_ref: 'PRD-114', currency: 'INR', amount_scale: 2, delta: '-451.20',
				delta_minor_units: -45120, classification: 'UNEXPLAINED',
				rationale: 'no component accounts for the difference',
				evidence: [
					{ kind: 'BASE', external_minor_units: 120000, shadow_minor_units: 165120, delta_minor_units: -45120 }
				],
				ml_hypotheses: [
					{ classification: 'INPUT_DIFFERENCE', confidence: 0.62, rationale: 'quantities look transposed',
					  supporting_fields: ['quantity_litres'], model_version: 'divergence-0.3.1', advisory: true }
				],
				status: 'OPEN', needs_review: true,
				created_at: '2026-08-20T04:15:00Z', updated_at: '2026-08-20T04:15:00Z'
			}
		]
	},
	'/shadowsettlement.v1.ShadowSettlementService/GetDivergence': null, // filled below
	'/canonical.v1.CanonicalService/ListIdentities': {
		identities: [
			{ id: 'i1', tenant_id: 't', source_system_id: 'LEGACY', entity_kind: 'PRODUCER',
			  external_id: '114', entity_id: 'PRD-114', method: 'MANUAL', valid_from: '2026-01-01T00:00:00Z',
			  valid_to: '9999-12-31T23:59:59Z', recorded_at: '2026-01-01T00:00:00Z' }
		]
	},
	'/canonical.v1.CanonicalService/ListConflicts': {
		slots: [
			{ id: 's1', tenant_id: 't', slot_key: 'PRD-114|CENTRE-2|2026-08-20|AM', origin_kind: 'DEVICE',
			  policy_id: 'pol-1', policy_version: 2, authoritative_ref: 'rec-a', status: 'CONFLICT',
			  incumbent_recorded_at: '2026-08-20T04:00:00Z', incumbent_quality: 80,
			  contenders: [{ source_ref: 'rec-b', origin: 'MANUAL_ENTRY', recorded_at: '2026-08-20T04:01:00Z',
			                 quality: 80, reason: 'equal quality under a last-wins policy' }],
			  values: { quantity_litres: '12.5', fat_percent: '4.1' } }
		]
	},
	'/balance.v1.BalanceService/ListWindows': {
		windows: [
			{ id: 'WIN01JGAVYA0000000000000001', tenant_id: 't', route_ref: 'RTE-NORTH',
			  period_start: '2026-08-20T00:00:00Z', period_end: '2026-08-21T00:00:00Z',
			  unit: 'L', status: 'RECONCILED',
			  created_at: '2026-08-21T02:00:00Z', updated_at: '2026-08-21T02:00:00Z' }
		]
	},
	'/balance.v1.BalanceService/ListFlows': {
		flows: [
			{ id: 'f1', tenant_id: 't', window_id: 'WIN01JGAVYA0000000000000001', flow_id: 'FL-IN',
			  from_node: '', to_node: 'CENTRE-2', to_node_kind: 'CENTRE',
			  measured: '4210.500', standard_uncertainty: '12.000', unmeasured: false,
			  created_at: '2026-08-21T01:00:00Z' },
			{ id: 'f2', tenant_id: 't', window_id: 'WIN01JGAVYA0000000000000001', flow_id: 'FL-LOSS',
			  from_node: 'CENTRE-2', to_node: '', measured: '', unmeasured: true,
			  created_at: '2026-08-21T01:00:00Z' }
		]
	},
	'/balance.v1.BalanceService/ListRuns': {
		runs: [
			{ id: 'RUN01JGAVYA0000000000000001', tenant_id: 't', window_id: 'WIN01JGAVYA0000000000000001',
			  converged: true, residual_before: '18.250', residual_after: '0.000',
			  model_version: 'reconciler-0.4.0', gross_error_threshold: 3,
			  suspect_flow_ids: ['FL-LOSS'],
			  flows: [
				{ flow_id: 'FL-IN', measured: '4210.500', reconciled: '4204.310',
				  adjustment: '-6.190', test_statistic: 0.52, gross_error: false, unmeasured: false },
				{ flow_id: 'FL-LOSS', measured: '', reconciled: '12.060',
				  adjustment: '12.060', test_statistic: 4.10, gross_error: true, unmeasured: true }
			  ],
			  created_at: '2026-08-21T02:00:00Z' }
		]
	},
	'/ingestion.v1.IngestionService/ListQuarantined': {
		records: [
			{ id: 'q1', tenant_id: 't', reason: 'SEQUENCE_REGRESSION', detail: 'sequence 4 follows 9',
			  device_id: 'DEV-01JGAVYA0000000000000001', generation: 2, external_session_id: 'BENCH-7-AM',
			  sequence: 4, payload_hash: 'sha256:9f2c', captured_at: '2026-08-20T05:00:00Z',
			  received_at: '2026-08-20T05:02:00Z', resolved: false }
		]
	}
};
GATEWAY['/shadowsettlement.v1.ShadowSettlementService/GetDivergence'] = {
	divergence: GATEWAY['/shadowsettlement.v1.ShadowSettlementService/ListDivergences'].divergences[0]
};

function serveStatic(req, res) {
	let file = path.join(ROOT, decodeURIComponent(new URL(req.url, 'http://x').pathname));
	if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(ROOT, 'index.html');
	res.writeHead(200, { 'content-type': TYPES[path.extname(file)] ?? 'application/octet-stream' });
	res.end(fs.readFileSync(file));
}

const site = http.createServer(serveStatic);
const gateway = http.createServer((req, res) => {
	if (req.method === 'OPTIONS') {
		res.writeHead(204, cors());
		return res.end();
	}
	const body = GATEWAY[req.url];
	let chunks = '';
	req.on('data', (c) => (chunks += c));
	req.on('end', () => {
		if (!body) {
			res.writeHead(404, { ...cors(), 'content-type': 'application/json' });
			return res.end(JSON.stringify({ code: 'not_found', message: 'no such procedure: ' + req.url }));
		}
		res.writeHead(200, { ...cors(), 'content-type': 'application/json' });
		res.end(JSON.stringify(body));
	});
});
const cors = () => ({
	'access-control-allow-origin': '*',
	'access-control-allow-headers': 'Content-Type, X-Tenant-ID, X-Request-ID',
	'access-control-allow-methods': 'POST, OPTIONS'
});

await new Promise((r) => site.listen(4173, r));
await new Promise((r) => gateway.listen(4174, r));

// Set CHROMIUM_PATH when the browser is installed somewhere Playwright does not
// look — a preinstalled image, for instance. Otherwise Playwright resolves it.
const browser = await chromium.launch(
	process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {}
);
const page = await browser.newPage();

const problems = [];
page.on('console', (m) => {
	if (m.type() === 'error') problems.push(`console: ${m.text()}`);
});
page.on('pageerror', (e) => problems.push(`pageerror: ${e.message}`));

await page.goto('http://localhost:4173/');
await page.evaluate(() =>
	localStorage.setItem(
		'gavya.settings.v1',
		JSON.stringify({ gatewayUrl: 'http://localhost:4174', tenantId: 'tnt-smoke', actor: 'Smoke' })
	)
);

const routes = [
	['/', 'Shadow settlement, at a glance'],
	['/integrity', 'Divergence queue'],
	['/integrity/01JGAVYADIVERGENCE0000001A', 'Divergence'],
	['/mapping', 'External identities'],
	['/mapping/conflicts', 'Collection slot conflicts'],
	['/quarantine', 'Quarantine'],
	['/balance', 'Mass balance'],
	['/balance/WIN01JGAVYA0000000000000001', 'Balance window']
];

for (const [route, heading] of routes) {
	await page.goto('http://localhost:4173' + route, { waitUntil: 'networkidle' });
	const h1 = await page.locator('h1').first().textContent();
	if (!h1 || !h1.includes(heading)) {
		problems.push(`${route}: heading was ${JSON.stringify(h1)}, expected to contain ${JSON.stringify(heading)}`);
	}
	const text = await page.locator('main').innerText();
	if (/Loading…$/.test(text.trim())) problems.push(`${route}: still loading after networkidle`);
	console.log(`${route.padEnd(34)} ${h1?.trim()}  [${text.length} chars]`);
}

// The data the pages actually rendered, so a silent empty state is visible.
await page.goto('http://localhost:4173/integrity', { waitUntil: 'networkidle' });
const rows = await page.locator('tbody tr').count();
if (rows !== 1) problems.push(`integrity queue rendered ${rows} rows, expected 1`);

await page.goto('http://localhost:4173/integrity/01JGAVYADIVERGENCE0000001A', { waitUntil: 'networkidle' });
const detail = await page.locator('main').innerText();
// The headline delta carries its currency, because that is the figure a
// reviewer decides on. The evidence rows stay bare, because repeating the
// symbol on every cell of a comparison table is noise — both are asserted so
// neither can drift into the other.
for (const must of [
	'-\u20B9451.20',
	'1200.00',
	'1651.20',
	'Unexplained',
	'Advisory',
	'divergence-0.3.1',
	'Record a decision'
]) {
	if (!detail.includes(must)) problems.push(`detail page is missing ${JSON.stringify(must)}`);
}
if (!detail.includes('settled for less')) problems.push('detail page did not state the direction of the difference');

await page.goto('http://localhost:4173/quarantine', { waitUntil: 'networkidle' });
const q = await page.locator('main').innerText();
if (!q.includes('Sequence regression')) problems.push('quarantine page did not label the reason');

await page.goto('http://localhost:4173/balance/WIN01JGAVYA0000000000000001', { waitUntil: 'networkidle' });
const bal = await page.locator('main').innerText();
for (const must of ['FL-IN', 'outside the network', '18.250', 'gross error', 'reconciler-0.4.0', 'Accept this reconciliation']) {
	if (!bal.includes(must)) problems.push(`balance window page is missing ${JSON.stringify(must)}`);
}
// An unmeasured leg must read as inferred, not as a measurement of nothing.
if (!bal.includes('inferred')) problems.push('an unmeasured flow was not marked inferred');

await browser.close();
site.close();
gateway.close();

if (problems.length) {
	console.error('\nPROBLEMS:\n' + problems.map((p) => ' - ' + p).join('\n'));
	process.exit(1);
}
console.log('\nsmoke: every route rendered with no console or page errors');
