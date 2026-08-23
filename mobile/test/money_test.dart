import 'package:flutter_test/flutter_test.dart';
import 'package:gavya_bench/src/capture/money.dart';

/// How an amount is written is not decoration. A yen shown with two decimals
/// misstates the figure, and an Indian operator checking a total written in
/// thousands has to count digits instead of reading groups.

void main() {
  test('a currency is written to its own number of decimals', () {
    expect(formatMoney(50, 'INR'), '₹50.00');
    expect(formatMoney(1250, 'JPY'), '¥1,250');
    expect(formatMoney(1.234, 'KWD'), 'KWD 1.234');
    expect(formatMoney(1234.5, 'EUR'), '€1,234.50');
  });

  test('a yen carries no decimals at all', () {
    // Not a rounding preference: ¥100.00 is a figure no Japanese invoice shows,
    // and it invites the reader to think there are sen to account for.
    expect(formatMoney(100, 'JPY'), '¥100');
    expect(scaleOf('JPY'), 0);
  });

  test('a dinar keeps its third decimal', () {
    expect(formatMoney(1.005, 'KWD'), 'KWD 1.005');
    expect(scaleOf('KWD'), 3);
    expect(scaleOf('BHD'), 3);
    expect(scaleOf('OMR'), 3);
  });

  test('the ordinary case is two decimals', () {
    for (final c in ['INR', 'USD', 'EUR', 'GBP', 'KES', 'BRL']) {
      expect(scaleOf(c), 2, reason: '$c should have two decimals');
    }
  });

  // The service sends the scale a figure was recorded at, and that is what the
  // figure means — the currency table is only a fallback.
  test('an explicit scale wins over the table', () {
    expect(formatMoney(100, 'JPY', scale: 2), '¥100.00');
    expect(formatMoney(1.5, 'INR', scale: 3), '₹1.500');
  });

  test('South Asian currencies group in lakhs rather than thousands', () {
    // An operator reading a handwritten sheet counts groups. 12,34,567 and
    // 1,234,567 are the same number and do not look like it.
    expect(formatMoney(1234567, 'INR'), '₹12,34,567.00');
    expect(formatMoney(100000, 'INR'), '₹1,00,000.00');
    expect(formatMoney(1000, 'INR'), '₹1,000.00');
    expect(formatMoney(1234567, 'PKR'), 'PKR 12,34,567.00');
  });

  test('everywhere else groups in thousands', () {
    expect(formatMoney(1234567, 'USD'), r'$1,234,567.00');
    expect(formatMoney(1234567, 'KES'), 'KSh1,234,567.00');
  });

  test('a negative amount keeps its sign in front of the symbol', () {
    expect(formatMoney(-451.20, 'INR'), '-₹451.20');
    expect(formatMoney(-1250, 'JPY'), '-¥1,250');
  });

  // A guessed symbol would be worse than no symbol: it would name the wrong
  // money. The code is always correct.
  test('a currency with no symbol in the table is shown by its code', () {
    expect(formatMoney(10, 'MZN'), 'MZN 10.00');
    expect(formatMoney(10, 'BWP'), 'BWP 10.00');
  });

  // ₨ is the Pakistani, Sri Lankan and Nepalese rupee alike; ¥ is the yen and
  // the yuan. A reader shown a shared symbol cannot tell which money it is, so
  // these carry their codes instead.
  test('a symbol shared between currencies is not used', () {
    for (final c in ['PKR', 'LKR', 'NPR', 'CNY']) {
      expect(formatMoney(10, c), startsWith('$c '),
          reason: '$c shares its symbol with another currency and must show its code');
    }
    // The yen keeps ¥ because nothing else in the table claims it.
    expect(formatMoney(10, 'JPY'), '¥10');
  });

  test('an unrecognised code still renders a readable figure', () {
    // Not a currency, but the operator must still see their number.
    expect(formatMoney(10, 'ZZZ'), 'ZZZ 10.00');
  });

  test('quantities group without pretending to be money', () {
    expect(formatQuantity('12.5', 'L'), '12.5 L');
    expect(formatQuantity('1234.5', 'L'), '1,234.5 L');
    expect(formatQuantity('12.5', ''), '12.5');
  });

  test('small amounts are not grouped', () {
    expect(formatMoney(1, 'INR'), '₹1.00');
    expect(formatMoney(999, 'INR'), '₹999.00');
    expect(formatMoney(0, 'JPY'), '¥0');
  });

  test('the case of the code does not matter', () {
    expect(formatMoney(50, 'inr'), '₹50.00');
    expect(scaleOf('jpy'), 0);
  });
}
