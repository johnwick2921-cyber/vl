package mentor

// ── B20 school-1 flip upgrade: armed only by an entry that LEAVES Tick ──────
//
// P2-N2 (mentor rescan, release #23): the level path and the box path armed
// SchoolOneSide the moment they built a school-1 entry the 5m trigger did not
// agree with — BEFORE the box ban, the trigger zone, the ORB gate, the
// fail-closed seed gate and the leg budget had their say. A refused intent
// still armed the upgrade, so a later trigger flip emitted ConfluenceUpgrade
// for an entry that never existed (10-14 08:24: a historical FTGH return the
// trigger zone refused armed the short side, and the bot logged a spurious
// "confluence upgrade IGNORED"; with an open short it would have switched that
// position from exit B to C). The candidates are now recorded where they are
// built and armed in Tick's leaving defer, from the intents that survived.

// schoolOneKey identifies one emitted entry intent across the later filters
// (none of them rewrites these fields).
type schoolOneKey struct {
	action   Action
	setup    string
	side     Side
	price    float64
	stop     float64
	refBarMs int64
	armID    string
}

func schoolOneKeyOf(in Intent) schoolOneKey {
	return schoolOneKey{action: in.Action, setup: in.Setup, side: in.Side, price: in.Price, stop: in.Stop, refBarMs: in.RefBarMs, armID: in.ArmID}
}

// armSchoolOne arms the B20 flip upgrade for every recorded school-1 candidate
// that is still among the intents leaving Tick. The last surviving candidate
// wins, as the in-loop assignment did.
func (e *Evaluator) armSchoolOne(out []Intent, cands []schoolOneKey) {
	if len(cands) == 0 || len(out) == 0 {
		return
	}
	want := make(map[schoolOneKey]bool, len(cands))
	for _, c := range cands {
		want[c] = true
	}
	for _, in := range out {
		if in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry {
			continue
		}
		if want[schoolOneKeyOf(in)] {
			e.State.SchoolOneSide = in.Side
			e.State.SchoolOneUpgraded = false
		}
	}
}
