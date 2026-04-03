package modelprofile

import "testing"

func TestResolveModel(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		fallback     string
		wantModel    string
		wantResolved string
	}{
		{name: "alias local", raw: "local", fallback: "", wantModel: ModelLocal, wantResolved: AliasLocal},
		{name: "alias spark", raw: "spark", fallback: "", wantModel: ModelSpark, wantResolved: AliasSpark},
		{name: "alias codex", raw: "codex", fallback: "", wantModel: ModelSpark, wantResolved: AliasSpark},
		{name: "full model", raw: ModelGPT, fallback: "", wantModel: ModelGPT, wantResolved: AliasGPT},
		{name: "mini alias", raw: "mini", fallback: "", wantModel: ModelMini, wantResolved: AliasMini},
		{name: "default alias", raw: "", fallback: AliasLocal, wantModel: ModelLocal, wantResolved: AliasLocal},
		{name: "custom passthrough", raw: "my-custom-model", fallback: "", wantModel: "my-custom-model", wantResolved: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveModel(tc.raw, tc.fallback); got != tc.wantModel {
				t.Fatalf("ResolveModel(%q, %q) = %q, want %q", tc.raw, tc.fallback, got, tc.wantModel)
			}
			if got := ResolveAlias(tc.raw, tc.fallback); got != tc.wantResolved {
				t.Fatalf("ResolveAlias(%q, %q) = %q, want %q", tc.raw, tc.fallback, got, tc.wantResolved)
			}
		})
	}
}

func TestMainThreadReasoningEffort(t *testing.T) {
	if got := MainThreadReasoningEffort(AliasLocal); got != ReasoningNone {
		t.Fatalf("local effort = %q, want %q", got, ReasoningNone)
	}
	if got := MainThreadReasoningEffort(AliasSpark); got != ReasoningLow {
		t.Fatalf("spark effort = %q, want %q", got, ReasoningLow)
	}
	if got := MainThreadReasoningEffort(AliasGPT); got != ReasoningHigh {
		t.Fatalf("gpt effort = %q, want %q", got, ReasoningHigh)
	}
	if got := MainThreadReasoningEffort(AliasMini); got != ReasoningHigh {
		t.Fatalf("mini effort = %q, want %q", got, ReasoningHigh)
	}
}

func TestAvailableReasoningEffortsByAlias(t *testing.T) {
	efforts := AvailableReasoningEffortsByAlias()
	if len(efforts) == 0 {
		t.Fatalf("expected efforts map")
	}
	for alias, expectation := range map[string][]string{
		AliasLocal: {ReasoningNone, ReasoningLow, ReasoningMedium, ReasoningHigh},
		AliasSpark: {ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningExtraHigh},
		AliasGPT:   {ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningExtraHigh},
		AliasMini:  {ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningExtraHigh},
	} {
		options, ok := efforts[alias]
		if !ok {
			t.Fatalf("missing alias %q", alias)
		}
		if len(options) != len(expectation) {
			t.Fatalf("alias %q option count = %d, want %d", alias, len(options), len(expectation))
		}
		for i := range expectation {
			if options[i] != expectation[i] {
				t.Fatalf("alias %q option[%d] = %q, want %q", alias, i, options[i], expectation[i])
			}
		}
	}
}

func TestNormalizeReasoningEffortLegacyExtraHigh(t *testing.T) {
	if got := NormalizeReasoningEffort(AliasSpark, "extra_high"); got != ReasoningExtraHigh {
		t.Fatalf("legacy effort normalize = %q, want %q", got, ReasoningExtraHigh)
	}
}

func TestLocalReasoningEffortToggle(t *testing.T) {
	if got := NormalizeReasoningEffort(AliasLocal, "none"); got != ReasoningNone {
		t.Fatalf("local none = %q, want %q", got, ReasoningNone)
	}
	if got := NormalizeReasoningEffort(AliasLocal, "low"); got != ReasoningLow {
		t.Fatalf("local low = %q, want %q", got, ReasoningLow)
	}
	if got := NormalizeReasoningEffort(AliasLocal, "medium"); got != ReasoningMedium {
		t.Fatalf("local medium = %q, want %q", got, ReasoningMedium)
	}
	if got := NormalizeReasoningEffort(AliasLocal, "high"); got != ReasoningHigh {
		t.Fatalf("local high = %q, want %q", got, ReasoningHigh)
	}
	// xhigh not supported for local, should fall back to default (none)
	if got := NormalizeReasoningEffort(AliasLocal, "xhigh"); got != ReasoningNone {
		t.Fatalf("local xhigh fallback = %q, want %q", got, ReasoningNone)
	}
}
