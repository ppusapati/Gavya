/// Writing money for an operator at a bench.
///
/// Two separate things decide how an amount reads. The currency fixes how many
/// decimals are real — a yen has none, a dinar has three — and showing ¥1200.00
/// is not a formatting preference but a misstatement of the figure. The
/// operator's locale fixes the grouping: an Indian operator reads 1,20,000 and
/// counts digits when shown 120,000.
///
/// This app carries no internationalisation package, so the grouping is done
/// here for the two conventions a dairy deployment actually meets. Adding a
/// third is adding a case, not rewriting anything.
library;

/// How many decimals a currency has, where it is not two.
const _minorUnits = <String, int>{
  'BHD': 3, 'IQD': 3, 'JOD': 3, 'KWD': 3, 'LYD': 3, 'OMR': 3, 'TND': 3,
  'CLF': 4, 'UYW': 4,
  'BIF': 0, 'CLP': 0, 'DJF': 0, 'GNF': 0, 'ISK': 0, 'JPY': 0, 'KMF': 0,
  'KRW': 0, 'PYG': 0, 'RWF': 0, 'UGX': 0, 'UYI': 0, 'VND': 0, 'VUV': 0,
  'XAF': 0, 'XDR': 0, 'XOF': 0, 'XPF': 0,
};

/// Symbols worth showing.
///
/// Only symbols that name one currency are here. ₨ is used for the Pakistani,
/// Sri Lankan and Nepalese rupee alike, and ¥ for both the yen and the yuan —
/// rendering two different currencies identically is worse than rendering
/// neither, because the reader has no way to tell they have been shown the
/// wrong money. Those currencies get their code, which is always correct if
/// less familiar. A code the table does not carry gets its code too; a guessed
/// symbol would name money the amount is not in.
const _symbols = <String, String>{
  'INR': '₹', 'USD': r'$', 'EUR': '€', 'GBP': '£', 'JPY': '¥',
  'KRW': '₩', 'NGN': '₦', 'PHP': '₱', 'THB': '฿', 'VND': '₫', 'BDT': '৳',
  'KES': 'KSh', 'UGX': 'USh', 'TZS': 'TSh',
  'ZAR': 'R', 'BRL': r'R$', 'TRY': '₺', 'RUB': '₽', 'ILS': '₪', 'UAH': '₴',
};

/// The number of decimals this currency is recorded to.
///
/// The service sends a scale alongside every amount it holds; prefer that. This
/// is for the places where only a code is to hand.
int scaleOf(String currency) => _minorUnits[currency.toUpperCase()] ?? 2;

/// Currencies whose home convention groups the last three digits and then in
/// pairs: 12,34,567 rather than 1,234,567.
const _lakhCrore = {'INR', 'PKR', 'BDT', 'LKR', 'NPR'};

/// Groups the integer part the way the currency's own readers group it.
String _group(String digits, String currency) {
  if (digits.length <= 3) return digits;
  if (_lakhCrore.contains(currency.toUpperCase())) {
    // The last three, then pairs. An operator checking a total against a
    // handwritten sheet is counting groups, not digits.
    final last3 = digits.substring(digits.length - 3);
    var rest = digits.substring(0, digits.length - 3);
    final parts = <String>[];
    while (rest.length > 2) {
      parts.insert(0, rest.substring(rest.length - 2));
      rest = rest.substring(0, rest.length - 2);
    }
    if (rest.isNotEmpty) parts.insert(0, rest);
    return '${parts.join(',')},$last3';
  }
  final parts = <String>[];
  var rest = digits;
  while (rest.length > 3) {
    parts.insert(0, rest.substring(rest.length - 3));
    rest = rest.substring(0, rest.length - 3);
  }
  if (rest.isNotEmpty) parts.insert(0, rest);
  return parts.join(',');
}

/// An amount written as money, at the currency's own precision.
///
/// [scale] comes from the record where the service supplies one, because the
/// stored scale is what the figure actually means.
String formatMoney(num amount, String currency, {int? scale}) {
  final digits = scale ?? scaleOf(currency);
  final code = currency.toUpperCase();
  final negative = amount < 0;

  final fixed = amount.abs().toStringAsFixed(digits);
  final dot = fixed.indexOf('.');
  final whole = dot < 0 ? fixed : fixed.substring(0, dot);
  final frac = dot < 0 ? '' : fixed.substring(dot);

  final symbol = _symbols[code];
  final body = '${_group(whole, code)}$frac';
  // A code rather than a symbol reads better with a space after it: "KWD 1.234"
  // is clearer than "KWD1.234", while "₹50.00" needs none.
  final head = symbol ?? '$code ';
  return '${negative ? '-' : ''}$head$body';
}

/// A quantity — litres, kilos — grouped the same way, with its unit.
String formatQuantity(String value, String unit) {
  final dot = value.indexOf('.');
  final whole = dot < 0 ? value : value.substring(0, dot);
  final frac = dot < 0 ? '' : value.substring(dot);
  final grouped = '${_group(whole, '')}$frac';
  return unit.isEmpty ? grouped : '$grouped $unit';
}
