import type { Classification, DivergenceStatus, QuarantineReason, SlotStatus } from './api';

/**
 * How the platform's vocabulary reads on screen.
 *
 * The tone of a chip carries meaning here: a reviewer scanning a queue decides
 * what to open from colour before reading a word. So "attention" is reserved for
 * what actually needs a person, and everything the platform already explained
 * stays quiet — including large differences, when the reason for them is known.
 */

export type Tone = 'calm' | 'attention' | 'critical' | 'neutral';

export function classificationTone(c: Classification | string): Tone {
	switch (c) {
		case 'MATCH':
			return 'calm';
		case 'ROUNDING_DIFFERENCE':
		case 'INPUT_DIFFERENCE':
		case 'POLICY_DIFFERENCE':
		case 'RECOVERY_DIFFERENCE':
			// Explained. The money still differs, but nobody has to work out why.
			return 'neutral';
		case 'UNEXPLAINED':
			return 'critical';
		case 'INSUFFICIENT_EVIDENCE':
			return 'attention';
		default:
			return 'neutral';
	}
}

export function statusTone(s: DivergenceStatus | string): Tone {
	switch (s) {
		case 'OPEN':
			return 'attention';
		case 'UNDER_REVIEW':
			return 'neutral';
		case 'ACCEPTED':
		case 'EXTERNAL_CONFIRMED':
		case 'SHADOW_CONFIRMED':
		case 'RESOLVED':
			return 'calm';
		default:
			return 'neutral';
	}
}

export function slotTone(s: SlotStatus | string): Tone {
	return s === 'CONFLICT' ? 'attention' : 'calm';
}

/** What each quarantine reason means, in a sentence a reviewer can act on. */
export const QUARANTINE_MEANING: Record<QuarantineReason, string> = {
	TRANSPORT_IDENTITY_CONFLICT:
		'A different record already occupies this device, generation, session and sequence. One of the two is not what it claims to be.',
	SEQUENCE_REGRESSION:
		'The sequence went backwards within a session, which a device that is counting forward cannot do.',
	UNTRUSTED_SESSION_IDENTITY:
		'The session identifier was not opened by this device, so nothing anchors the sequence it uses.',
	STALE_GENERATION:
		'The device has since rolled its generation. This record belongs to a sequence space that has been closed.',
	SESSION_NOT_ACCEPTING: 'The session was already closed when this record arrived.'
};

/** SCREAMING_SNAKE from the wire, read as a phrase. */
export function label(v: string): string {
	if (!v) return '';
	const words = v.toLowerCase().split('_');
	return words[0].charAt(0).toUpperCase() + words[0].slice(1) + (words.length > 1 ? ' ' + words.slice(1).join(' ') : '');
}

/** An RFC3339 instant as a person reads it, in their own timezone. */
export function instant(iso: string | undefined): string {
	if (!iso) return '—';
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return iso;
	return d.toLocaleString(undefined, {
		year: 'numeric',
		month: 'short',
		day: '2-digit',
		hour: '2-digit',
		minute: '2-digit'
	});
}

/** An RFC3339 instant as the value an <input type="datetime-local"> expects. */
export function toLocalInput(d: Date): string {
	const pad = (n: number) => String(n).padStart(2, '0');
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** A datetime-local value back to RFC3339, which is what the services parse. */
export function fromLocalInput(v: string): string {
	if (!v) return '';
	const d = new Date(v);
	return Number.isNaN(d.getTime()) ? '' : d.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

/** Identifiers are long and the middle of one is never what distinguishes it. */
export function shortId(id: string): string {
	return id.length <= 12 ? id : `${id.slice(0, 8)}…${id.slice(-4)}`;
}

/** Whether a validity interval is the open-ended one the services write. */
export function isOpenEnded(validTo: string): boolean {
	if (!validTo) return true;
	const year = new Date(validTo).getUTCFullYear();
	return Number.isNaN(year) || year >= 9999;
}
