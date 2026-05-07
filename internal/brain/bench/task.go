package bench

// Task is one bench task type: folder-note authoring, entity triage,
// sleep-judge editorial, scout web verification, mixed-note compression.
// Fixtures hold the per-instance ground truth; Score runs on the
// model's output.
type Task interface {
	ID() string
	PromptFile() string
	Fixtures() ([]Fixture, error)
	Score(f Fixture, output string) (score float64, passes bool, rationale string)
}

// Fixture is one input + ground-truth pair.
type Fixture struct {
	ID         string
	Packet     string            // user prompt the model sees
	Expected   map[string]string // free-form per-task expectations
	References []string          // optional reference outputs
}
