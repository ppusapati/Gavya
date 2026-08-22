import 'package:flutter/material.dart';

/// The bench app is used outdoors, one-handed, often in poor light, by someone
/// whose attention is on the milk and not on a screen. So the type is large,
/// the targets are big, and the only saturated colour is the one that means
/// something is wrong.

const _verdigris = Color(0xFF2E6B5E);
const _signal = Color(0xFF98570F);
const _critical = Color(0xFF8E3129);

ThemeData benchTheme(Brightness brightness) {
  final scheme = ColorScheme.fromSeed(
    seedColor: _verdigris,
    brightness: brightness,
  ).copyWith(error: brightness == Brightness.light ? _critical : const Color(0xFFCD6355));

  return ThemeData(
    colorScheme: scheme,
    useMaterial3: true,
    visualDensity: VisualDensity.comfortable,
    textTheme: const TextTheme(
      headlineSmall: TextStyle(fontWeight: FontWeight.w700, letterSpacing: -0.4),
      titleMedium: TextStyle(fontWeight: FontWeight.w600),
      bodyMedium: TextStyle(fontSize: 15, height: 1.45),
      bodySmall: TextStyle(fontSize: 13, height: 1.4),
    ),
    inputDecorationTheme: const InputDecorationTheme(
      border: OutlineInputBorder(),
      isDense: false,
      contentPadding: EdgeInsets.symmetric(horizontal: 14, vertical: 16),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: const Size.fromHeight(52),
        textStyle: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(48)),
    ),
    cardTheme: CardThemeData(
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(6),
        side: BorderSide(color: scheme.outlineVariant),
      ),
      margin: const EdgeInsets.only(bottom: 12),
    ),
  );
}

/// The colour that means a person has to look at something.
Color attentionColour(BuildContext context) =>
    Theme.of(context).brightness == Brightness.light ? _signal : const Color(0xFFD5964A);
