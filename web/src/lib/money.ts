/**
 * Rendering money for a person.
 *
 * Two things decide how an amount is written, and they are not the same thing.
 * The *currency* fixes how many decimals are real: a yen has none, a dinar has
 * three, and showing ¥1,200.00 or KD 1.23 is not a formatting preference but a
 * misstatement of the figure. The *reader's locale* fixes the grouping and the
 * separators: 1,20,000 in India, 120 000 in France, 120,000 almost everywhere
 * else — and an Indian accountant reading lakhs written in thousands has to
 * count digits to check a total.
 *
 * Intl knows both, so nothing here reimplements either. What this file does is
 * make sure the currency is always passed, because an amount rendered without
 * one is the bug this platform spent a whole layer removing from the database.
 */

/** How many decimals a currency actually has, where it is not two. */
const MINOR_UNITS: Record<string, number> = {
	BHD: 3, IQD: 3, JOD: 3, KWD: 3, LYD: 3, OMR: 3, TND: 3,
	CLF: 4, UYW: 4,
	BIF: 0, CLP: 0, DJF: 0, GNF: 0, ISK: 0, JPY: 0, KMF: 0, KRW: 0,
	PYG: 0, RWF: 0, UGX: 0, UYI: 0, VND: 0, VUV: 0,
	XAF: 0, XDR: 0, XOF: 0, XPF: 0
};

/**
 * The number of decimals this currency is recorded to.
 *
 * The services send the scale alongside every amount, so prefer that. This
 * exists for the places where only a code is to hand.
 */
export function scaleOf(currency: string): number {
	return MINOR_UNITS[currency?.toUpperCase()] ?? 2;
}

/**
 * An amount, written the way the reader expects to see money written.
 *
 * `scale` comes from the record where the platform supplies one, because the
 * stored scale is what the figure actually means; falling back to the currency
 * table is for display-only paths.
 */
export function formatMoney(
	amount: number,
	currency: string,
	options: { scale?: number; locale?: string; compact?: boolean } = {}
): string {
	const digits = options.scale ?? scaleOf(currency);
	try {
		return new Intl.NumberFormat(options.locale, {
			style: 'currency',
			currency,
			minimumFractionDigits: digits,
			maximumFractionDigits: digits,
			notation: options.compact ? 'compact' : 'standard'
		}).format(amount);
	} catch {
		// An unrecognised code must not blank the figure. Showing the number
		// beside the code it claims is less useful than a symbol and far more
		// useful than nothing.
		return `${amount.toFixed(digits)} ${currency ?? ''}`.trim();
	}
}

/**
 * Minor units as money, e.g. 5000 at scale 2 in INR as "₹50.00".
 *
 * The integrity services carry amounts as minor units with an explicit scale,
 * because that is the only representation that cannot lose a fils. This is the
 * one place that turns them back into something to read.
 */
export function formatMinorUnits(
	minorUnits: number,
	scale: number,
	currency: string,
	locale?: string
): string {
	return formatMoney(minorUnits / Math.pow(10, scale), currency, { scale, locale });
}

/**
 * Minor units as a bare decimal, with no currency at all.
 *
 * For the rare case where the currency is already stated beside the figure and
 * repeating it would be noise — a column header, say. Deliberately separate
 * from formatMoney so that omitting the currency is always a decision.
 */
export function formatMinorUnitsPlain(minorUnits: number, scale: number): string {
	const negative = minorUnits < 0;
	const digits = Math.abs(minorUnits).toString().padStart(scale + 1, '0');
	const whole = digits.slice(0, digits.length - scale);
	const frac = scale > 0 ? '.' + digits.slice(digits.length - scale) : '';
	return `${negative ? '-' : ''}${whole}${frac}`;
}

/** A quantity — litres, kilos — with the reader's own grouping. */
export function formatQuantity(value: number, unit: string, locale?: string): string {
	const n = new Intl.NumberFormat(locale, { maximumFractionDigits: 3 }).format(value);
	return unit ? `${n} ${unit}` : n;
}

/**
 * An amount the service sent as an exact decimal string, grouped for a reader
 * and never parsed.
 *
 * The functions above take a number, which is what the integrity screens had to
 * hand and is safe for the magnitudes involved. The money path does not have to
 * settle for that: settlement, pooling and procurement all send the amount as a
 * decimal string beside its currency, and that string is the figure a member can
 * be shown and can check.
 *
 * So this groups the integer part itself and appends the fraction verbatim.
 * Nothing here calls Number, which means nothing here can round — and the one
 * screen where a rounded figure would matter most is a producer's statement.
 *
 * The grouping is the reader's, via Intl on the integer part alone, because an
 * Indian accountant reading lakhs written in thousands has to count digits to
 * check a total.
 */
export function formatExact(value: string | undefined, currency: string, locale?: string): string {
	const raw = (value ?? '').trim();
	if (raw === '') return '—';

	const negative = raw.startsWith('-');
	const unsigned = negative ? raw.slice(1) : raw;
	const [whole = '0', fraction = ''] = unsigned.split('.');

	// Only digits get grouped. Anything else is handed back as it arrived rather
	// than mangled into something that looks like a number and is not.
	if (!/^\d+$/.test(whole) || (fraction !== '' && !/^\d+$/.test(fraction))) {
		return currency ? `${currency} ${raw}` : raw;
	}

	let grouped = whole;
	try {
		// BigInt, so a whole part beyond Number's safe range groups correctly
		// rather than silently losing its last digits.
		grouped = new Intl.NumberFormat(locale, { useGrouping: true }).format(BigInt(whole));
	} catch {
		grouped = whole;
	}

	const body = fraction === '' ? grouped : `${grouped}.${fraction}`;
	const signed = negative ? `-${body}` : body;
	return currency ? `${currency} ${signed}` : signed;
}
