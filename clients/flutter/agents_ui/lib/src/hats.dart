/// Pixel art for the 13 hats: a still version and the frames each cycles
/// through while its agent works, for the critter and for the onigiri
/// (narrower, sitting over its tip). Rows are placed at ([HatFrame.x],
/// [HatFrame.top]) in sprite cells; "a" = the hat's colour, "b" its shade,
/// "c" a highlight, "d" near-black; extras are single cells. The bandana's
/// knot and tails sit on the left. Mirrors the web dashboard's HAT_ART
/// (backend/internal/web/static/deck-core.js): edit both together.
library;

class HatFrame {
  const HatFrame({
    required this.x,
    required this.top,
    required this.rows,
    this.extras = const [],
  });

  final int x;
  final int top;
  final List<String> rows;
  final List<(int, int, String)> extras;
}

class HatArt {
  const HatArt({
    required this.ms,
    required this.critterStill,
    required this.critter,
    required this.onigiriStill,
    required this.onigiri,
  });

  /// How long each working frame shows.
  final int ms;
  final HatFrame critterStill;
  final List<HatFrame> critter;
  final HatFrame onigiriStill;
  final List<HatFrame> onigiri;
}

const hatArt = <String, HatArt>{
  'hard hat': HatArt(
    ms: 400,
    critterStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '....aaaaa.....',
        '...acaaaaa....',
        '..aaaaaaaaa...',
        '.bbbbbbbbbbbb.',
      ],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '....aaaaa.....',
          '...acaaaaa....',
          '..aaaaccaaa...',
          '.bbbbbbbbbbbb.',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '....aaaaa.....',
          '...acaaaaa....',
          '..aaaabbaaa...',
          '.bbbbbbbbbbbb.',
        ],
        extras: [],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: -3,
      rows: ['.....aaaa.....', '....acaaaa....', '...bbbbbbbb...'],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: -3,
        rows: ['.....aaaa.....', '....acccaa....', '...bbbbbbbb...'],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['.....aaaa.....', '....acbbaa....', '...bbbbbbbb...'],
        extras: [],
      ),
    ],
  ),
  'cap': HatArt(
    ms: 300,
    critterStill: HatFrame(
      x: -3,
      top: -3,
      rows: ['........aaaaaa', '.......aaaaacaa', 'bbbbbbbbbbbbbbb'],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: -3,
        top: -3,
        rows: ['........aaaaaa', '.......aaaaacaa', 'bbbbbbbbbbbbbbb'],
        extras: [],
      ),
      HatFrame(
        x: -3,
        top: -3,
        rows: ['........aaaaaa', '.......aaaaacaa', 'bbbbbbbbbbbbbbb'],
        extras: [],
      ),
      HatFrame(
        x: -3,
        top: -3,
        rows: ['........aaaaaa', '.......aaaaacaa', '...bbbbbbbbbbbb'],
        extras: [(-3, 0, 'b'), (-2, 0, 'b')],
      ),
      HatFrame(
        x: -3,
        top: -3,
        rows: ['........aaaaaa', '.......aaaaacaa', 'bbbbbbbbbbbbbbb'],
        extras: [],
      ),
    ],
    onigiriStill: HatFrame(
      x: -1,
      top: -3,
      rows: ['......aaaa', '.....aaacaa', 'bbbbbbbbbbbb'],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: -1,
        top: -3,
        rows: ['......aaaa', '.....aaacaa', 'bbbbbbbbbbbb'],
        extras: [],
      ),
      HatFrame(
        x: -1,
        top: -3,
        rows: ['......aaaa', '.....aaacaa', 'bbbbbbbbbbbb'],
        extras: [],
      ),
      HatFrame(
        x: -1,
        top: -3,
        rows: ['......aaaa', '.....aaacaa', '..bbbbbbbbbb'],
        extras: [(-1, 0, 'b')],
      ),
      HatFrame(
        x: -1,
        top: -3,
        rows: ['......aaaa', '.....aaacaa', 'bbbbbbbbbbbb'],
        extras: [],
      ),
    ],
  ),
  'beanie': HatArt(
    ms: 350,
    critterStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '.....cc.......',
        '....aaaaaa....',
        '...aaaaaaaa...',
        '..babababab...',
      ],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....cc.......',
          '....aaaaaa....',
          '...aaaaaaaa...',
          '..babababab...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['....aaaaaa....', '...aaaaaaaa...', '..babababab...'],
        extras: [(5, -5, 'c'), (6, -5, 'c')],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '......cc......',
        '.....aaaa.....',
        '....aaaaaa....',
        '...babababa...',
      ],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '......cc......',
          '.....aaaa.....',
          '....aaaaaa....',
          '...babababa...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['.....aaaa.....', '....aaaaaa....', '...babababa...'],
        extras: [(6, -5, 'c'), (7, -5, 'c')],
      ),
    ],
  ),
  'helmet': HatArt(
    ms: 280,
    critterStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '....aaaaaa....',
        '...aaaaaaaa...',
        '..aaaacaaaaa..',
        '..bbbbbbbbbb..',
      ],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '....aaaaaa....',
          '...aaaaaaaa...',
          '..acaaaaaaaa..',
          '..bbbbbbbbbb..',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '....aaaaaa....',
          '...aaaaaaaa...',
          '..aaaaacaaaa..',
          '..bbbbbbbbbb..',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '....aaaaaa....',
          '...aaaaaaaa...',
          '..aaaaaaaaca..',
          '..bbbbbbbbbb..',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '....aaaaaa....',
          '...aaaaaaaa...',
          '..aaaaaaaaaa..',
          '..bbbbbbbbbb..',
        ],
        extras: [],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: -3,
      rows: ['.....aaaa.....', '....aacaaa....', '...bbbbbbbb...'],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: -3,
        rows: ['.....aaaa.....', '....acaaaa....', '...bbbbbbbb...'],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['.....aaaa.....', '....aaacaa....', '...bbbbbbbb...'],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['.....aaaa.....', '....aaaaca....', '...bbbbbbbb...'],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['.....aaaa.....', '....aaaaaa....', '...bbbbbbbb...'],
        extras: [],
      ),
    ],
  ),
  'wizard': HatArt(
    ms: 300,
    critterStill: HatFrame(
      x: 0,
      top: -5,
      rows: [
        '........a.....',
        '.......aa.....',
        '......aaca....',
        '.....aaaaaa...',
        '..bbbbbbbbbb..',
      ],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '........a.....',
          '.......aa.....',
          '......aaca....',
          '.....aaaaaa...',
          '..bbbbbbbbbb..',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '.........a....',
          '.......aa.....',
          '......aaca....',
          '.....aaaaaa...',
          '..bbbbbbbbbb..',
        ],
        extras: [(11, -6, 'c')],
      ),
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '........a.....',
          '.......aa.....',
          '......aaca....',
          '.....aaaaaa...',
          '..bbbbbbbbbb..',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '.......a......',
          '.......aa.....',
          '......aaca....',
          '.....aaaaaa...',
          '..bbbbbbbbbb..',
        ],
        extras: [],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: -5,
      rows: [
        '......aa......',
        '......aa......',
        '.....acaa.....',
        '....aaaaaa....',
        '...bbbbbbbb...',
      ],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '......a.......',
          '......aa......',
          '.....acaa.....',
          '....aaaaaa....',
          '...bbbbbbbb...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '.......a......',
          '......aa......',
          '.....acaa.....',
          '....aaaaaa....',
          '...bbbbbbbb...',
        ],
        extras: [(9, -6, 'c')],
      ),
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '......a.......',
          '......aa......',
          '.....acaa.....',
          '....aaaaaa....',
          '...bbbbbbbb...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '.....a........',
          '......aa......',
          '.....acaa.....',
          '....aaaaaa....',
          '...bbbbbbbb...',
        ],
        extras: [],
      ),
    ],
  ),
  'beret': HatArt(
    ms: 300,
    critterStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '......b.......',
        '...aaaaaaaa...',
        '..aaaaaaaaaaa.',
        '...bbbbbbbb...',
      ],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '......b.......',
          '...aaaaaaaa...',
          '..aaaaaaaaaaa.',
          '...bbbbbbbb...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.......b......',
          '...aaaaaaaa...',
          '..aaaaaaaaaaa.',
          '...bbbbbbbb...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '......b.......',
          '...aaaaaaaa...',
          '..aaaaaaaaaaa.',
          '...bbbbbbbb...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....b........',
          '...aaaaaaaa...',
          '..aaaaaaaaaaa.',
          '...bbbbbbbb...',
        ],
        extras: [],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '......bb......',
        '....aaaaaa....',
        '..aaaaaaaaaa..',
        '....bbbbbb....',
      ],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '......b.......',
          '....aaaaaa....',
          '..aaaaaaaaaa..',
          '....bbbbbb....',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.......b......',
          '....aaaaaa....',
          '..aaaaaaaaaa..',
          '....bbbbbb....',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '......b.......',
          '....aaaaaa....',
          '..aaaaaaaaaa..',
          '....bbbbbb....',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....b........',
          '....aaaaaa....',
          '..aaaaaaaaaa..',
          '....bbbbbb....',
        ],
        extras: [],
      ),
    ],
  ),
  'chef': HatArt(
    ms: 450,
    critterStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '...cc.cc.cc...',
        '..cccccccccc..',
        '...cccccccc...',
        '...aaaaaaaa...',
      ],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '...cc.cc.cc...',
          '..cccccccccc..',
          '...cccccccc...',
          '...aaaaaaaa...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '....cccccc....',
          '..cccccccccc..',
          '..cccccccccc..',
          '...cccccccc...',
          '...aaaaaaaa...',
        ],
        extras: [],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '....cccccc....',
        '....cccccc....',
        '.....cccc.....',
        '....aaaaaa....',
      ],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '....cccccc....',
          '....cccccc....',
          '.....cccc.....',
          '....aaaaaa....',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '.....cccc.....',
          '....cccccc....',
          '....cccccc....',
          '.....cccc.....',
          '....aaaaaa....',
        ],
        extras: [],
      ),
    ],
  ),
  'crown': HatArt(
    ms: 280,
    critterStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '...a..aa..a...',
        '...aa.aa.aa...',
        '...aaaaaaaa...',
        '...acaacaaca..',
      ],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '...c..aa..a...',
          '...aa.aa.aa...',
          '...aaaaaaaa...',
          '...acaacaaca..',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '...a..ca..a...',
          '...aa.aa.aa...',
          '...aaaaaaaa...',
          '...acaacaaca..',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '...a..ac..a...',
          '...aa.aa.aa...',
          '...aaaaaaaa...',
          '...acaacaaca..',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '...a..aa..a...',
          '...aa.aa.aa...',
          '...aaaaaaaa...',
          '...acaacaaca..',
        ],
        extras: [],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: -3,
      rows: ['....a.aa.a....', '....aaaaaa....', '....acaaca....'],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: -3,
        rows: ['....c.aa.a....', '....aaaaaa....', '....acaaca....'],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['....a.ca.a....', '....aaaaaa....', '....acaaca....'],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['....a.ac.a....', '....aaaaaa....', '....acaaca....'],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['....a.aa.a....', '....aaaaaa....', '....acaaca....'],
        extras: [],
      ),
    ],
  ),
  'headphones': HatArt(
    ms: 320,
    critterStill: HatFrame(
      x: 0,
      top: -3,
      rows: ['....aaaaaa....', '...a......a...', '..a........a..'],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -3,
        rows: ['....aaaaaa....', '...a......a...', '..a........a..'],
        extras: [(-2, -1, 'c'), (-1, -2, 'c')],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['....aaaaaa....', '...a......a...', '..a........a..'],
        extras: [(-2, -2, 'c'), (-1, -3, 'c')],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['....aaaaaa....', '...a......a...', '..a........a..'],
        extras: [(-3, -3, 'c'), (-2, -4, 'c'), (-1, -1, 'c')],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['....aaaaaa....', '...a......a...', '..a........a..'],
        extras: [(-2, -2, 'c')],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: -3,
      rows: ['.....aaaa.....', '....a....a....', '...a......a...'],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: -3,
        rows: ['.....aaaa.....', '....a....a....', '...a......a...'],
        extras: [(-1, -1, 'c'), (0, -2, 'c')],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['.....aaaa.....', '....a....a....', '...a......a...'],
        extras: [(-1, -2, 'c'), (0, -3, 'c')],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['.....aaaa.....', '....a....a....', '...a......a...'],
        extras: [(-2, -3, 'c'), (-1, -4, 'c'), (0, -1, 'c')],
      ),
      HatFrame(
        x: 0,
        top: -3,
        rows: ['.....aaaa.....', '....a....a....', '...a......a...'],
        extras: [(-1, -2, 'c')],
      ),
    ],
  ),
  'propeller': HatArt(
    ms: 120,
    critterStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '..cccc.aaaa...',
        '.......d......',
        '....ababab....',
        '...aaaaaaaa...',
      ],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '..cccc.aaaa...',
          '.......d......',
          '....ababab....',
          '...aaaaaaaa...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '....cc.aa.....',
          '.......d......',
          '....ababab....',
          '...aaaaaaaa...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '..aaaa.cccc...',
          '.......d......',
          '....ababab....',
          '...aaaaaaaa...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '....aa.cc.....',
          '.......d......',
          '....ababab....',
          '...aaaaaaaa...',
        ],
        extras: [],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '...ccc..aaa...',
        '......dd......',
        '.....abab.....',
        '....aaaaaa....',
      ],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '...ccc..aaa...',
          '......dd......',
          '.....abab.....',
          '....aaaaaa....',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....cc.aa....',
          '......dd......',
          '.....abab.....',
          '....aaaaaa....',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '...aaa..ccc...',
          '......dd......',
          '.....abab.....',
          '....aaaaaa....',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....aa.cc....',
          '......dd......',
          '.....abab.....',
          '....aaaaaa....',
        ],
        extras: [],
      ),
    ],
  ),
  'top hat': HatArt(
    ms: 450,
    critterStill: HatFrame(
      x: 0,
      top: -5,
      rows: [
        '...aaaaaaaa...',
        '...aaaaaaaa...',
        '...aaaaaaaa...',
        '...bbbbbbbb...',
        '.aaaaaaaaaaaa.',
      ],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '...aaaaaaaa...',
          '...aaaaaaaa...',
          '...aaaaaaaa...',
          '...bbbbbbbb...',
          '.aaaaaaaaaaaa.',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '...aaaaaaaa...',
          '...aaaaaaaa...',
          '...aaaaaaaa...',
          '...bbbbbbbb...',
          '.aaaaaaaaaaaa.',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -6,
        rows: [
          '...aaaaaaaa...',
          '...aaaaaaaa...',
          '...aaaaaaaa...',
          '...bbbbbbbb...',
          '.aaaaaaaaaaaa.',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '...aaaaaaaa...',
          '...aaaaaaaa...',
          '...aaaaaaaa...',
          '...bbbbbbbb...',
          '.aaaaaaaaaaaa.',
        ],
        extras: [],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '.....aaaa.....',
        '.....aaaa.....',
        '.....bbbb.....',
        '...aaaaaaaa...',
      ],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....aaaa.....',
          '.....aaaa.....',
          '.....bbbb.....',
          '...aaaaaaaa...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....aaaa.....',
          '.....aaaa.....',
          '.....bbbb.....',
          '...aaaaaaaa...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -5,
        rows: [
          '.....aaaa.....',
          '.....aaaa.....',
          '.....bbbb.....',
          '...aaaaaaaa...',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....aaaa.....',
          '.....aaaa.....',
          '.....bbbb.....',
          '...aaaaaaaa...',
        ],
        extras: [],
      ),
    ],
  ),
  'cowboy': HatArt(
    ms: 350,
    critterStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '.....aaaa.....',
        '....aabbaa....',
        'b...aaaaaa...b',
        'bbbbbbbbbbbbbb',
      ],
      extras: [],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....aaaa.....',
          '....aabbaa....',
          'b...aaaaaa...b',
          'bbbbbbbbbbbbbb',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....aaaa.....',
          'b...aabbaa....',
          'b...aaaaaa....',
          '.bbbbbbbbbbbbb',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....aaaa.....',
          '....aabbaa....',
          'b...aaaaaa...b',
          'bbbbbbbbbbbbbb',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '.....aaaa.....',
          '....aabbaa...b',
          '....aaaaaa...b',
          'bbbbbbbbbbbbb.',
        ],
        extras: [],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: -4,
      rows: [
        '......aa......',
        '.....abba.....',
        '.b..aaaaaa..b.',
        '.bbbbbbbbbbbb.',
      ],
      extras: [],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '......aa......',
          '.....abba.....',
          '.b..aaaaaa..b.',
          '.bbbbbbbbbbbb.',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '......aa......',
          '.b...abba.....',
          '.b..aaaaaa....',
          '..bbbbbbbbbbb.',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '......aa......',
          '.....abba.....',
          '.b..aaaaaa..b.',
          '.bbbbbbbbbbbb.',
        ],
        extras: [],
      ),
      HatFrame(
        x: 0,
        top: -4,
        rows: [
          '......aa......',
          '.....abba...b.',
          '....aaaaaa..b.',
          '.bbbbbbbbbbb..',
        ],
        extras: [],
      ),
    ],
  ),
  'bandana': HatArt(
    ms: 200,
    critterStill: HatFrame(
      x: 0,
      top: -1,
      rows: ['..aaaaaaaaaa..', '..acacacacac..'],
      extras: [(1, -1, 'b'), (0, -2, 'b'), (0, 0, 'b'), (-1, 0, 'b')],
    ),
    critter: [
      HatFrame(
        x: 0,
        top: -1,
        rows: ['..aaaaaaaaaa..', '..acacacacac..'],
        extras: [(1, -1, 'b'), (0, -2, 'b'), (0, 0, 'b'), (-1, 0, 'b')],
      ),
      HatFrame(
        x: 0,
        top: -1,
        rows: ['..aaaaaaaaaa..', '..acacacacac..'],
        extras: [
          (1, -1, 'b'),
          (0, -1, 'b'),
          (-1, -2, 'b'),
          (0, 0, 'b'),
          (-1, 1, 'b'),
        ],
      ),
      HatFrame(
        x: 0,
        top: -1,
        rows: ['..aaaaaaaaaa..', '..acacacacac..'],
        extras: [
          (1, -1, 'b'),
          (0, -2, 'b'),
          (-1, -2, 'b'),
          (0, 0, 'b'),
          (-1, 0, 'b'),
        ],
      ),
    ],
    onigiriStill: HatFrame(
      x: 0,
      top: 0,
      rows: ['...aaaaaaaa...', '..acacacacac..'],
      extras: [(2, 0, 'b'), (1, -1, 'b'), (1, 1, 'b'), (0, 1, 'b')],
    ),
    onigiri: [
      HatFrame(
        x: 0,
        top: 0,
        rows: ['...aaaaaaaa...', '..acacacacac..'],
        extras: [(2, 0, 'b'), (1, -1, 'b'), (1, 1, 'b'), (0, 1, 'b')],
      ),
      HatFrame(
        x: 0,
        top: 0,
        rows: ['...aaaaaaaa...', '..acacacacac..'],
        extras: [
          (2, 0, 'b'),
          (1, 0, 'b'),
          (0, -1, 'b'),
          (1, 1, 'b'),
          (0, 2, 'b'),
        ],
      ),
      HatFrame(
        x: 0,
        top: 0,
        rows: ['...aaaaaaaa...', '..acacacacac..'],
        extras: [
          (2, 0, 'b'),
          (1, -1, 'b'),
          (0, -1, 'b'),
          (1, 1, 'b'),
          (0, 1, 'b'),
        ],
      ),
    ],
  ),
};
