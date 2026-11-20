package engine

// Choices is what a class can build a damage rotation from. The standard
// rotation keeps a fixed set of DoTs up (the class module's Rotations); for
// the rotation guide every combination is a candidate, because which spells
// pay depends on the fight: a DoT that outlives the enemy does not.
type Choices struct {
	Optional []Action // each one with and without, in priority order
	Fillers  []*Spell // one of them, or none: then the wand fills the time and the mana regenerates
	// Recover is a filler that returns health (Drain Life), offered next to
	// every other filler as the spell to cast when the health is too low to
	// convert.
	Recover *Spell
	Tail    []Action // always at the end: a spell that trades health for mana, the wand
}

// Rotations expands the choices into every candidate rotation.
func (c Choices) Rotations() [][]Action {
	subsets := [][]Action{nil}
	for _, opt := range c.Optional {
		for _, s := range subsets[:len(subsets):len(subsets)] {
			subsets = append(subsets, append(append([]Action(nil), s...), opt))
		}
	}
	fillers := append(append([]*Spell(nil), c.Fillers...), nil)
	var out [][]Action
	for _, filler := range fillers {
		recovers := []*Spell{nil}
		if c.Recover != nil && filler != nil && filler.ID != c.Recover.ID {
			recovers = append(recovers, c.Recover)
		}
		for _, rec := range recovers {
			for _, s := range subsets {
				actions := sortedLike(c.Optional, s)
				if filler != nil {
					actions = append(actions, Action{Kind: "filler", Spell: filler})
				}
				if rec != nil {
					actions = append(actions, Action{Kind: "recover", Spell: rec})
				}
				out = append(out, append(actions, c.Tail...))
			}
		}
	}
	return out
}

// sortedLike returns the subset in the order of the full list.
func sortedLike(all, subset []Action) []Action {
	in := map[int]bool{}
	for _, a := range subset {
		in[a.Spell.ID] = true
	}
	out := make([]Action, 0, len(subset))
	for _, a := range all {
		if in[a.Spell.ID] {
			out = append(out, a)
		}
	}
	return out
}
