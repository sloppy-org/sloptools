package modelprofile

import "strings"

const (
	AliasLocal = "local"
	AliasSpark = "spark"
	AliasGPT   = "gpt"
	AliasMini  = "mini"
	AliasCodex = "codex"

	ModelLocal = "local"
	ModelSpark = "gpt-5.3-codex-spark"
	ModelGPT   = "gpt-5.4"
	ModelMini  = "gpt-5-mini"

	ReasoningNone      = "none"
	ReasoningMinimal   = "minimal"
	ReasoningLow       = "low"
	ReasoningMedium    = "medium"
	ReasoningHigh      = "high"
	ReasoningExtraHigh = "xhigh"
)

const legacyReasoningExtraHigh = "extra_high"

var aliasToModel = map[string]string{
	AliasLocal: ModelLocal,
	AliasSpark: ModelSpark,
	AliasGPT:   ModelGPT,
	AliasMini:  ModelMini,
	AliasCodex: ModelSpark,
}

var modelToAlias = map[string]string{
	strings.ToLower(ModelLocal): AliasLocal,
	strings.ToLower(ModelSpark): AliasSpark,
	strings.ToLower(ModelGPT):   AliasGPT,
	strings.ToLower(ModelMini):  AliasMini,
}

var modelReasoningEfforts = map[string][]string{
	AliasLocal: {ReasoningNone, ReasoningLow, ReasoningMedium, ReasoningHigh},
	AliasSpark: {ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningExtraHigh},
	AliasGPT:   {ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningExtraHigh},
	AliasMini:  {ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningExtraHigh},
	AliasCodex: {ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningExtraHigh},
}

func SupportedAliases() []string {
	return []string{AliasLocal, AliasSpark, AliasCodex, AliasGPT, AliasMini}
}

func SupportedModels() []string {
	return []string{ModelLocal, ModelSpark, ModelGPT, ModelMini}
}

func NormalizeAlias(raw string) string {
	key := strings.TrimSpace(strings.ToLower(raw))
	switch key {
	case AliasCodex:
		return AliasSpark
	}
	if _, ok := aliasToModel[key]; ok {
		return key
	}
	return ""
}

func AliasForModel(raw string) string {
	if alias := NormalizeAlias(raw); alias != "" {
		return alias
	}
	key := strings.TrimSpace(strings.ToLower(raw))
	if key == "" {
		return ""
	}
	if alias, ok := modelToAlias[key]; ok {
		return alias
	}
	return ""
}

func ResolveAlias(raw, fallback string) string {
	if alias := AliasForModel(raw); alias != "" {
		return alias
	}
	if alias := AliasForModel(fallback); alias != "" {
		return alias
	}
	return ""
}

func ModelForAlias(alias string) string {
	clean := NormalizeAlias(alias)
	if clean == "" {
		return ""
	}
	return aliasToModel[clean]
}

func ResolveModel(raw, fallbackAlias string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		if model := ModelForAlias(fallbackAlias); model != "" {
			return model
		}
		return ""
	}
	if model := ModelForAlias(trimmed); model != "" {
		return model
	}
	if alias := AliasForModel(trimmed); alias != "" {
		return aliasToModel[alias]
	}
	return trimmed
}

func MainThreadReasoningEffort(alias string) string {
	switch NormalizeAlias(alias) {
	case AliasLocal:
		return ReasoningNone
	case AliasSpark:
		return ReasoningLow
	case AliasGPT, AliasMini:
		return ReasoningHigh
	default:
		return ReasoningNone
	}
}

func AvailableReasoningEffortsByAlias() map[string][]string {
	out := map[string][]string{}
	for alias, options := range modelReasoningEfforts {
		copied := append([]string(nil), options...)
		out[alias] = copied
	}
	return out
}

func ReasoningEffortsForAlias(alias string) []string {
	if options, ok := modelReasoningEfforts[NormalizeAlias(alias)]; ok {
		return append([]string(nil), options...)
	}
	return append([]string(nil), modelReasoningEfforts[AliasLocal]...)
}

func NormalizeReasoningEffort(alias, rawEffort string) string {
	effort := canonicalReasoningEffort(rawEffort)
	for _, candidate := range ReasoningEffortsForAlias(alias) {
		if candidate == effort {
			return effort
		}
	}
	defaultEffort := MainThreadReasoningEffort(alias)
	if defaultEffort == "" {
		return ""
	}
	return defaultEffort
}

func MainThreadReasoningParamsForEffort(alias, effort string) map[string]interface{} {
	effort = NormalizeReasoningEffort(alias, effort)
	return ReasoningParamsForEffort(effort)
}

func ReasoningParamsForEffort(effort string) map[string]interface{} {
	effort = canonicalReasoningEffort(effort)
	switch effort {
	case ReasoningNone, ReasoningMinimal, ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningExtraHigh:
		return map[string]interface{}{"effort": effort}
	default:
		return nil
	}
}

func canonicalReasoningEffort(raw string) string {
	clean := strings.TrimSpace(strings.ToLower(raw))
	switch clean {
	case legacyReasoningExtraHigh:
		return ReasoningExtraHigh
	default:
		return clean
	}
}

func MainThreadReasoningParams(alias string) map[string]interface{} {
	return MainThreadReasoningParamsForEffort(alias, "")
}

// ModelSystemHints returns model-specific system prompt additions.
// Slopshell keeps Codex/GPT/Spark prompts clean and relies on model-native behavior.
func ModelSystemHints(alias string) string {
	_ = alias
	return ""
}
