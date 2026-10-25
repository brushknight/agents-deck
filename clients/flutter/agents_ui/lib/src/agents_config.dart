import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';

/// How a client reaches the agents daemon.
///
/// A paired network device (the desk panel) holds a bundle from
/// `agentctl pair <name> --bundle FILE`: the listener's https URL, the deck's
/// CA certificate, its own client certificate and key, and the listener's
/// certificate fingerprint (accepted when the host name isn't one the
/// certificate covers). The connection is TLS 1.3 with the client certificate;
/// the server is verified against the CA.
///
/// On the computer running agentctl, [localAgentsConfig] uses the daemon's unix
/// socket instead: nothing crosses the network.
///
/// Each app says where it keeps its bundle (the desk panel passes its own
/// paths to [load]); the default is `~/.config/agents-deck/agents.json`, and
/// `AGENTS_CONFIG=<file>` overrides it.
class AgentsConfig {
  const AgentsConfig({
    required this.url,
    this.name = '',
    this.ca = '',
    this.cert = '',
    this.key = '',
    this.fingerprint,
    this.socketPath,
  });

  /// The daemon's unix socket on this machine.
  AgentsConfig.unix(String path)
    : this(
        url: Uri(scheme: 'http', host: 'localhost'),
        socketPath: path,
      );

  /// Daemon base URL, no trailing slash (http://localhost for the socket).
  final Uri url;

  /// The device's name on the deck.
  final String name;

  /// PEM: the deck's CA, this device's certificate and private key.
  final String ca;
  final String cert;
  final String key;

  /// SHA-256 of the listener's certificate (32 bytes), or null.
  final List<int>? fingerprint;

  /// Set for the local unix socket; then there's no TLS.
  final String? socketPath;

  bool get isLocal => socketPath != null;

  static const fileName = 'agents.json';

  /// Validates a bundle and normalizes it; throws [FormatException] with a
  /// short, user-facing reason.
  static AgentsConfig fromJson(Map<String, dynamic> j) {
    final u = j['url'] is String
        ? Uri.tryParse((j['url'] as String).trim())
        : null;
    if (u == null || u.scheme != 'https' || u.host.isEmpty) {
      throw const FormatException('url must be https://host:port');
    }
    String pem(String field, String label) {
      final v = j[field];
      if (v is! String || !v.contains('$label-----')) {
        throw FormatException(
          'no $field: pair again with `agentctl pair <name> --bundle FILE`',
        );
      }
      return v.trim();
    }

    final ca = pem('ca', 'CERTIFICATE');
    final cert = pem('cert', 'CERTIFICATE');
    final key = pem('key', 'PRIVATE KEY');
    List<int>? fp;
    if (j['fingerprint'] case final String f when f.trim().isNotEmpty) {
      fp = parseFingerprint(f);
      if (fp == null) {
        throw const FormatException(
          'fingerprint must be sha256:AB:CD:… (32 bytes)',
        );
      }
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
      name: j['name'] is String ? j['name'] as String : '',
      ca: ca,
      cert: cert,
      key: key,
      fingerprint: fp,
    );
  }

  Map<String, dynamic> toJson() => {
    'name': name,
    'url': url.toString(),
    'ca': ca,
    'cert': cert,
    'key': key,
    if (fingerprint case final f?) 'fingerprint': formatFingerprint(f),
    'socket': ?socketPath,
  };

  /// `url` + path segments (each segment is escaped on its own, so an agent
  /// id can never walk the path).
  Uri endpoint(List<String> segments) => url.replace(
    pathSegments: [...url.pathSegments.where((s) => s.isNotEmpty), ...segments],
  );

  /// Resolution order: `$AGENTS_CONFIG` → [paths] (default:
  /// ~/.config/agents-deck/agents.json). Returns the config, or null with
  /// the reason when nothing usable exists (null reason = not configured).
  static (AgentsConfig?, String?) load({
    Map<String, String>? env,
    List<String>? paths,
  }) {
    final e = env ?? Platform.environment;
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

/// The daemon on this same computer, over its unix socket
/// (`~/.local/share/agents-terminal/agentd.sock`, or under `$AGENTSTERM_HOME`;
/// see [daemonSocketPath]): only your user can open it, and nothing crosses
/// the network. Null with a reason when agentctl isn't running.
(AgentsConfig?, String?) localAgentsConfig({Map<String, String>? env}) {
  final e = env ?? Platform.environment;
  final home =
      e['AGENTSTERM_HOME'] ??
      (e['HOME'] == null ? null : '${e['HOME']}/.local/share/agents-terminal');
  if (home == null) return (null, 'no home directory');
  final sock = daemonSocketPath(home, env: e);
  if (FileSystemEntity.typeSync(sock) != FileSystemEntityType.unixDomainSock) {
    return (
      null,
      Platform.isLinux
          ? 'agentctl is not running here: run '
                '`systemctl --user start agents-deck` or `agentctl serve`'
          : 'agentctl is not running here: run `agentctl install`',
    );
  }
  return (AgentsConfig.unix(sock), null);
}

/// Where the daemon puts its socket for data directory [dataDir], as
/// `paths.Socket` does in the Go code: `<dataDir>/agentd.sock`, or, when that
/// path is too long for a unix socket (macOS caps it at 104 bytes), the temp
/// directory (`$TMPDIR`, else /tmp) with a name made from a hash of [dataDir].
String daemonSocketPath(String dataDir, {Map<String, String>? env}) {
  final p = '$dataDir/agentd.sock';
  if (utf8.encode(p).length < 100) return p;
  final e = env ?? Platform.environment;
  var tmp = e['TMPDIR'] ?? '';
  if (tmp.isEmpty) tmp = '/tmp';
  while (tmp.length > 1 && tmp.endsWith('/')) {
    tmp = tmp.substring(0, tmp.length - 1);
  }
  final sum = sha256.convert(utf8.encode(dataDir)).bytes.sublist(0, 4);
  final hex = sum.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  return '$tmp/agentsterm-$hex.sock';
}

/// "sha256:AB:CD:…", "ab cd …", "abcd…" → 32 bytes; null if malformed.
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
