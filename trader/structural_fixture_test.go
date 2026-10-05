package trader

import (
	"time"
	"vl/kernel"
	"vl/store"
)

// Fixed, explicit machine identities for unrelated historical gate fixtures.
// No production detector, ranking rule or entry gate is replaced by this helper.
func structuralTestIdentity(price float64, label string) kernel.PlanLevel {
	l := identityTestLevel(time.Date(2026, 9, 10, 20, 0, 0, 0, kernel.CTLocation()), price)
	l.Label = label
	l.Kind = kernel.LevelKind(label)
	return kernel.CandidateIdentity(l)
}
func structuralTestPolicy(c *store.StrategyConfig, buffer float64) {
	if c.DayPlan == nil {
		c.DayPlan = &store.DayPlanConfig{}
	}
	c.DayPlan.StructuralStop = &store.StructuralStopConfig{BufferPoints: &buffer}
}

type structuralTestZone struct {
	anchor, lo, hi float64
	label          string
}

func structuralTestMap(d *kernel.PlanDoc, zones ...structuralTestZone) {
	d.Zones = &kernel.LevelZoneMap{At: time.Date(2026, 9, 10, 20, 0, 0, 0, kernel.CTLocation())}
	d.IdentityLevels = nil
	for i, l := range d.Levels {
		identity := structuralTestIdentity(l.Price, l.Label)
		d.IdentityLevels = append(d.IdentityLevels, identity)
		identity.Grade = l.Grade
		identity.Instruction = l.Instruction
		d.Levels[i] = identity
		for si := range d.Scenarios {
			sc := &d.Scenarios[si]
			if sc.Arm != nil && sc.Arm.Entry == l.Price {
				sc.LevelID = identity.ID
			}
		}
	}
	for _, z := range zones {
		lo, hi := z.lo, z.hi
		d.Zones.Zones = append(d.Zones.Zones, kernel.LevelZone{Anchor: z.anchor, Lo: &lo, Hi: &hi, Sources: []kernel.ZoneSource{{Price: z.anchor, Label: z.label, TF: "1m"}}})
	}
}
