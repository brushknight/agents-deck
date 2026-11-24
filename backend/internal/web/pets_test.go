package web_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/web"
)

// The pets' pixel art (pets.js), run in node when it is installed: every
// stage and mood draws, at one scale, with few shapes and no inline styles.
func TestPetArt(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	for _, name := range []string{"deck-core.js", "pets.js"} {
		b, err := fs.ReadFile(web.Static(), name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type": "module"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "check.js"), []byte(petCheck), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, filepath.Join(dir, "check.js")).CombinedOutput(); err != nil {
		t.Errorf("%v\n%s", err, out)
	}
}

const petCheck = `
import { petSVG, STAGES, levelAt, petKind, heartSVG } from './pets.js';
const fail = (m) => { console.error(m); process.exitCode = 1; };
// The same numbers as the service (internal/pets Threshold).
if (levelAt(0) !== 0 || levelAt(1) !== 50000 || levelAt(5) !== 122070 || levelAt(40) !== 300926554) fail('levels differ from the service');
if (petKind({ stage: 'strong' }) !== 'Strong Builder' || petKind({ stage: 'grown' }) !== 'Builder' || petKind({ stage: 'egg' }) !== 'Egg') fail('pet kinds');
if (heartSVG(true) === heartSVG(false) || /style=/.test(heartSVG(true))) fail('hearts');
if (petSVG(null) !== '') fail('no pet: no drawing');
const hat = { shape: 'top hat', color: 'yellow' };
const box = (s) => /viewBox="([^"]+)"/.exec(s)[1];
const dark = (s) => (s.match(/M-?\d+ -?\d+h1v1h-1z(?=[^"]*" fill="#0A0A0A")/g) || []).length;
let vb = '';
for (const stage of STAGES) for (const mood of ['fed', 'hungry', 'weak', 'asleep']) for (const idle of [false, true]) {
  const what = stage + ' ' + mood + (idle ? ' idle' : '');
  const pet = { stage, mood, ate: 40000 };
  const plain = petSVG(pet, { hat, idle, working: true });
  const edged = petSVG(pet, { hat, idle, working: true, outline: true });
  for (const s of [plain, edged]) {
    if (/style=/.test(s)) fail(what + ': inline style');
    vb = vb || box(s);
    if (box(s) !== vb) fail(what + ': drawn at another scale');
    const shapes = (s.match(/<(path|rect)/g) || []).length;
    if (shapes > 40) fail(what + ': ' + shapes + ' shapes');
  }
  if (dark(edged) <= dark(plain)) fail(what + ': no dark edge on an orange tile');
}
if (!petSVG({ stage: 'grown', mood: 'hungry' }, { hat }).includes('#BDB8AD')) fail('hungry: no light grey mouth');
if (!petSVG({ stage: 'legend', mood: 'fed' }, { hat }).includes('class="ptw pt0"')) fail('legend: no stars');
if (petSVG({ stage: 'legend', mood: 'weak' }, { hat }).includes('prs')) fail('weak: no aura');
if (!petSVG({ stage: 'kid', mood: 'asleep' }, { hat }).includes('pfall')) fail('asleep: no ice');
if (!petSVG({ stage: 'kid', mood: 'fed' }, { hat, waking: true }).includes('pws ps4')) fail('waking: no shards');
const sil = petSVG({ stage: 'mighty', mood: 'fed' }, { hat, sil: '#3B3A36' });
if (new Set(sil.match(/fill="#[0-9A-Fa-f]{6}"/g)).size !== 1 || sil.includes('prs')) fail('silhouette: one colour, no aura');
`
