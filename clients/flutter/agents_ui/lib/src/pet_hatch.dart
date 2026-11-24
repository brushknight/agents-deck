/// Hatching a ready egg from the deck: the egg shakes and bursts, the
/// service is asked to hatch it (`POST /v1/agents/{id}/pet/hatch`, as the
/// web does), and the new pet pops in. The motion is the web's (phx and
/// pborn in settings.css).
library;

import 'dart:async';

import 'package:flutter/widgets.dart';

import 'agents_client.dart';
import 'agents_model.dart';

/// Builds the pet that is shown: [pet] is the egg while it shakes, then the
/// hatched pet; [hatch] is null unless [pet] is an egg ready to hatch.
typedef PetHatchBuilder =
    Widget Function(BuildContext context, AgentPet pet, VoidCallback? hatch);

/// Holds an agent's pet and hatches it when asked: [builder] gets a
/// callback for a ready egg (tap it, or a "hatch it" button). While it
/// hatches, the egg shakes for 1.4 s, flashes white and bursts; once the
/// service sends the hatched pet it pops in (0.6 s). With reduce motion on
/// it just changes. [onProblem] hears why a hatch was refused.
class PetHatcher extends StatefulWidget {
  const PetHatcher({
    super.key,
    required this.pet,
    required this.onHatch,
    required this.builder,
    this.onProblem,
  });

  final AgentPet pet;
  final Future<AgentsResult> Function() onHatch;
  final PetHatchBuilder builder;
  final ValueChanged<String>? onProblem;

  @override
  State<PetHatcher> createState() => _PetHatcherState();
}

class _PetHatcherState extends State<PetHatcher> with TickerProviderStateMixin {
  late final _burst = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1400),
  );
  late final _born = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 600),
  );

  /// The egg as it was when the hatch started (shown until the burst ends).
  AgentPet? _egg;
  bool _busy = false;

  bool get _still => MediaQuery.maybeDisableAnimationsOf(context) ?? false;

  @override
  void didUpdateWidget(PetHatcher old) {
    super.didUpdateWidget(old);
    // The hatched pet arrived after the burst ended: pop it in now.
    if (_egg != null && !widget.pet.isEgg && !_burst.isAnimating) _popIn();
  }

  Future<void> _hatch() async {
    if (_busy) return;
    _busy = true;
    setState(() => _egg = widget.pet);
    if (!_still) unawaited(_burst.forward(from: 0).then((_) => _burstDone()));
    final r = await widget.onHatch();
    if (!mounted) return;
    if (!r.isOk) {
      _burst.reset();
      setState(() {
        _egg = null;
        _busy = false;
      });
      widget.onProblem?.call(r.message ?? 'it couldn\'t hatch');
      return;
    }
    if (_still) _popIn();
  }

  void _burstDone() {
    if (!mounted || _egg == null) return;
    if (!widget.pet.isEgg) _popIn();
  }

  void _popIn() {
    _burst.reset();
    setState(() {
      _egg = null;
      _busy = false;
    });
    if (!_still) _born.forward(from: 0);
  }

  @override
  void dispose() {
    _burst.dispose();
    _born.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final egg = _egg;
    final pet = egg ?? widget.pet;
    final ready = egg == null && pet.isEgg && pet.ready;
    final child = widget.builder(context, pet, ready ? _hatch : null);
    return AnimatedBuilder(
      animation: Listenable.merge([_burst, _born]),
      child: child,
      builder: (context, child) {
        if (_burst.isAnimating || (egg != null && _burst.value > 0)) {
          return _shake(_burst.value, child!);
        }
        if (_born.isAnimating) return _pop(_born.value, child!);
        return child!;
      },
    );
  }

  /// The burst (phx): side to side in whole pixels, then it swells, goes
  /// white and fades.
  static Widget _shake(double t, Widget child) {
    const xs = [0.0, -2.0, 2.0, -2.0, 2.0, -3.0, 3.0];
    final i = (t * 10).floor();
    final dx = i < xs.length ? xs[i] : 0.0;
    var scale = 1.0, bright = 1.0, opacity = 1.0;
    if (t >= 0.7) {
      final k = t < 0.85 ? (t - 0.7) / 0.15 : 1.0;
      scale = t < 0.85 ? 1 + 0.15 * k : 1.15 + 0.15 * (t - 0.85) / 0.15;
      bright = 1 + 3 * k;
      opacity = t < 0.85 ? 1 : 1 - (t - 0.85) / 0.15;
    }
    return Opacity(
      opacity: opacity.clamp(0.0, 1.0),
      child: Transform(
        alignment: const Alignment(0, 0.6),
        transform: Matrix4.translationValues(dx, 0, 0)
          ..scaleByDouble(scale, scale, 1, 1),
        child: ColorFiltered(
          colorFilter: ColorFilter.matrix([
            bright, 0, 0, 0, 0, //
            0, bright, 0, 0, 0, //
            0, 0, bright, 0, 0, //
            0, 0, 0, 1, 0,
          ]),
          child: child,
        ),
      ),
    );
  }

  /// The new pet pops in (pborn): four steps from half size to a little
  /// big, then its size.
  static Widget _pop(double t, Widget child) {
    const steps = [(0.5, 0.0), (0.75, 1.0), (1.1, 1.0), (1.0, 1.0)];
    final (scale, opacity) = steps[(t * 4).floor().clamp(0, 3)];
    return Opacity(
      opacity: opacity,
      child: Transform.scale(
        scale: scale,
        alignment: const Alignment(0, 0.6),
        child: child,
      ),
    );
  }
}
