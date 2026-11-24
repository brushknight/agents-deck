/// Hatching a ready egg from the deck: the egg shakes and bursts, the
/// service is asked to hatch it (`POST /v1/agents/{id}/pet/hatch`, as the
/// web does), and the new pet pops in. The motion is the web's (phx and
/// pborn in settings.css), in whole cells.
library;

import 'dart:async';

import 'package:flutter/widgets.dart';

import 'agents_client.dart';
import 'agents_model.dart';
import 'pet_art.dart';

/// Builds the pet that is shown: [pet] is the egg while it shakes, then the
/// hatched pet; [hatch] is null unless [pet] is an egg ready to hatch.
typedef PetHatchBuilder =
    Widget Function(BuildContext context, AgentPet pet, VoidCallback? hatch);

/// Holds an agent's pet and hatches it when asked: [builder] gets a
/// callback for a ready egg (tap it, or a "hatch it" button). While it
/// hatches, the egg shakes 1 then 2 cells for 1.4 s, flashes white and is
/// gone; once the service sends the hatched pet it rises in from 2 cells
/// below (0.6 s). With reduce motion on it just changes. [onProblem] hears why a hatch was refused.
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
    // The pet under it moves by whole cells (PetHatchScope): the web's
    // phx and pborn.
    return AnimatedBuilder(
      animation: Listenable.merge([_burst, _born]),
      child: child,
      builder: (context, child) => PetHatchScope(
        step: _burst.isAnimating || (egg != null && _burst.value > 0)
            ? PetHatchStep.hatch(_burst.value)
            : _born.isAnimating
            ? PetHatchStep.born(_born.value)
            : null,
        child: child!,
      ),
    );
  }
}
