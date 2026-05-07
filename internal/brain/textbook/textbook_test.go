package textbook

import "testing"

func TestClassifyConceptOnly_PureTextbook_Reject(t *testing.T) {
	c := New()
	v, pat := c.Classify(Note{
		Path:  "topics/boltzmann-equation.md",
		Title: "Boltzmann equation",
		Body:  "The Boltzmann equation describes the statistical behavior of a thermodynamic system.\n",
	})
	if v != VerdictReject {
		t.Fatalf("want reject, got %s (matched=%q)", v, pat)
	}
	if pat == "" {
		t.Fatalf("want matched pattern, got empty")
	}
}

func TestClassifyMixedNote_KeepWithCompression(t *testing.T) {
	c := New()
	body := "The banana regime arises in low-collisionality plasmas.\n\nSee [[projects/NEO-RT]] for the implementation.\n"
	v, pat := c.Classify(Note{
		Path:  "topics/banana-regime.md",
		Title: "Banana regime",
		Body:  body,
	})
	if v != VerdictCompress {
		t.Fatalf("want compress, got %s (matched=%q)", v, pat)
	}
	if pat == "" {
		t.Fatalf("want matched pattern, got empty")
	}
}

func TestClassifyCanonicalEntity_NeverArchived(t *testing.T) {
	c := New()
	v, _ := c.Classify(Note{
		Path:  "people/winfried-kernbichler.md",
		Title: "Winfried Kernbichler",
		Body:  "Head of plasma physics group.\n",
	})
	if v != VerdictKeep {
		t.Fatalf("canonical entity must be kept, got %s", v)
	}
}

func TestClassifyStrategicFlag_OverridesDenyList(t *testing.T) {
	c := New()
	v, _ := c.Classify(Note{
		Path:      "topics/mhd.md",
		Title:     "MHD",
		Body:      "Pure textbook content.\n",
		Strategic: true,
	})
	if v != VerdictKeep {
		t.Fatalf("strategic note must be kept, got %s", v)
	}
}

func TestClassifyDailyCadence_OverridesDenyList(t *testing.T) {
	c := New()
	v, _ := c.Classify(Note{
		Path:    "topics/runge-kutta.md",
		Title:   "Runge–Kutta",
		Body:    "Pure textbook content.\n",
		Cadence: "daily",
	})
	if v != VerdictKeep {
		t.Fatalf("daily-cadence note must be kept, got %s", v)
	}
}

func TestClassifyOutsideDenyList_AlwaysKeep(t *testing.T) {
	c := New()
	v, _ := c.Classify(Note{
		Path:  "topics/local-method-x.md",
		Title: "Local method X",
		Body:  "Pure textbook content but not on deny-list.\n",
	})
	if v != VerdictKeep {
		t.Fatalf("non-listed note must be kept, got %s", v)
	}
}

func TestHasLocalAnchor_ProjectsLink(t *testing.T) {
	if !hasLocalAnchor("see [[projects/NEO-RT]] for details") {
		t.Fatal("projects/ link should count as local anchor")
	}
}

func TestHasLocalAnchor_OnlyExternal(t *testing.T) {
	if hasLocalAnchor("see [Wikipedia](https://en.wikipedia.org/wiki/MHD)") {
		t.Fatal("external link should not count")
	}
}

func TestHasLocalAnchor_TopicsLinkDoesNotCount(t *testing.T) {
	if hasLocalAnchor("see [[topics/another-textbook]]") {
		t.Fatal("topics/ link should not count as local anchor")
	}
}

func TestPatternsLoadedFromEmbed(t *testing.T) {
	c := New()
	patterns := c.Patterns()
	if len(patterns) < 30 {
		t.Fatalf("expected many patterns from embedded deny-list, got %d", len(patterns))
	}
	for _, p := range []string{"boltzmann-equation", "runge-kutta", "mpi"} {
		found := false
		for _, q := range patterns {
			if q == p {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q in deny-list", p)
		}
	}
}
