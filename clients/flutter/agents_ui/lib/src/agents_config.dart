import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';

/// Pairing values for the agents daemon, exactly what `agentctl pair`
/// prints: `{"url": "https://…:7341", "token": "…", "fingerprint":
/// "sha256:AB:CD:…"}`.
///
/// Each app says where it keeps them (the desk panel passes its own paths
/// to [load]); the default is `~/.config/agents-deck/agents.json`.
/// `AGENTS_URL` + `AGENTS_TOKEN` + `AGENTS_FINGERPRINT` (all three) or
/// `AGENTS_CONFIG=<file>` override the files. On the Mac running agentctl,
/// [localAgentsConfig] needs no file at all.
class AgentsConfig {
  const AgentsConfig({
    required this.url,
    required this.token,
    required this.fingerprint,
  });

  /// Daemon base URL (https only), no trailing slash.
  final Uri url;
  final String token;

  /// SHA-256 of the server certificate's DER bytes (32 bytes).
  final List<int> fingerprint;

  static const fileName = 'agents.json';

  /// Validates and normalizes; throws [FormatException] with a short,
  /// user-facing reason.
  static AgentsConfig parse({
    required Object? url,
    required Object? token,
    required Object? fingerprint,
  }) {
    final u = url is String ? Uri.tryParse(url.trim()) : null;
    if (u == null || u.scheme != 'https' || u.host.isEmpty) {
      throw const FormatException('url must be https://host:port');
    }
    final t = token is String ? token.trim() : '';
    if (t.isEmpty) throw const FormatException('token required');
    final fp = fingerprint is String ? parseFingerprint(fingerprint) : null;
    if (fp == null) {
      throw const FormatException(
        'fingerprint must be sha256:AB:CD:… (32 bytes)',
      );
    }
    var path = u.path;
    while (path.endsWith('/')) {
      path = path.substring(0, path.length - 1);
    }
    return AgentsConfig(
      url: Uri(
        scheme: 'https',
        host: u.host,
        port: u.hasPort ? u.port : null,
        path: path,
      ),
      token: t,
      fingerprint: fp,
    );
  }

  static AgentsConfig fromJson(Map<String, dynamic> j) =>
      parse(url: j['url'], token: j['token'], fingerprint: j['fingerprint']);

  Map<String, dynamic> toJson() => {
    'url': url.toString(),
    'token': token,
    'fingerprint': formatFingerprint(fingerprint),
  };

  /// `url` + path segments (each segment is escaped on its own, so an agent
  /// id can never walk the path).
  Uri endpoint(List<String> segments) => url.replace(
    pathSegments: [...url.pathSegments.where((s) => s.isNotEmpty), ...segments],
  );

  /// Resolution order: env triple → `$AGENTS_CONFIG` → [paths] (default:
  /// ~/.config/agents-deck/agents.json). Returns the config, or null with
  /// the reason when nothing usable exists (null reason = not configured).
  static (AgentsConfig?, String?) load({
    Map<String, String>? env,
    List<String>? paths,
  }) {
    final e = env ?? Platform.environment;
    final eu = e['AGENTS_URL'],
        et = e['AGENTS_TOKEN'],
        ef = e['AGENTS_FINGERPRINT'];
    if (eu != null && et != null && ef != null) {
      try {
        return (parse(url: eu, token: et, fingerprint: ef), null);
      } on FormatException catch (x) {
        return (null, 'AGENTS_* env: ${x.message}');
      }
    }
    final candidates =
        paths ??
        [
          ?e['AGENTS_CONFIG'],
          if (e['HOME'] case final h?) '$h/.config/agents-deck/$fileName',
        ];
    for (final path in candidates) {
      final f = File(path);
      String raw;
      try {
        if (!f.existsSync()) continue;
        raw = f.readAsStringSync();
      } catch (_) {
        continue; // unreadable: try the next location
      }
      try {
        final j = jsonDecode(raw);
        if (j is! Map<String, dynamic>) {
          throw const FormatException('not an object');
        }
        return (fromJson(j), null);
      } on FormatException catch (x) {
        return (null, 'agents.json: ${x.message}');
      }
    }
    return (null, null);
  }
}

/// "sha256:AB:CD:…", "ab cd …", "abcd…" → 32 bytes; null if malformed.
/// Pairing with the daemon on this same Mac, straight from its own files
/// (`~/.local/share/agents-terminal`, or `$AGENTSTERM_HOME`): the panel
/// listener on 127.0.0.1, its device token and the pinned fingerprint of its
/// certificate. Null with a reason when agentctl isn't installed or paired.
(AgentsConfig?, String?) localAgentsConfig({
  Map<String, String>? env,
  int port = 7341,
}) {
  final e = env ?? Platform.environment;
  final home =
      e['AGENTSTERM_HOME'] ??
      (e['HOME'] == null ? null : '${e['HOME']}/.local/share/agents-terminal');
  if (home == null) return (null, 'no home directory');
  String read(String name) => File('$home/$name').readAsStringSync().trim();
  try {
    final token = read('device-token');
    final pem = read('tls-cert.pem');
    final body = pem
        .split('\n')
        .where((l) => l.isNotEmpty && !l.startsWith('-----'))
        .join();
    final der = base64.decode(body);
    return (
      AgentsConfig(
        url: Uri(scheme: 'https', host: '127.0.0.1', port: port),
        token: token,
        fingerprint: sha256.convert(der).bytes,
      ),
      null,
    );
  } on FileSystemException {
    return (
      null,
      'agentctl is not set up here yet: run `agentctl install` and `agentctl pair`',
    );
  } on FormatException {
    return (null, 'agentctl certificate is unreadable');
  }
}

List<int>? parseFingerprint(String s) {
  var v = s.trim();
  if (v.toLowerCase().startsWith('sha256:')) v = v.substring(7);
  if (v.toLowerCase().startsWith('sha256 ')) v = v.substring(7);
  v = v.replaceAll(RegExp(r'[\s:]'), '');
  if (!RegExp(r'^[0-9a-fA-F]{64}$').hasMatch(v)) return null;
  return [
    for (var i = 0; i < 64; i += 2) int.parse(v.substring(i, i + 2), radix: 16),
  ];
}

/// Canonical form: `sha256:AB:CD:…` (uppercase, colon-separated).
String formatFingerprint(List<int> bytes) =>
    'sha256:${bytes.map((b) => b.toRadixString(16).padLeft(2, '0').toUpperCase()).join(':')}';

/// True when SHA-256(der) equals [expected]. Constant-time over the digest.
bool fingerprintMatches(List<int> der, List<int> expected) {
  final got = sha256.convert(der).bytes;
  if (expected.length != got.length) return false;
  var diff = 0;
  for (var i = 0; i < got.length; i++) {
    diff |= got[i] ^ expected[i];
  }
  return diff == 0;
}
