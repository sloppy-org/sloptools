package bench

import (
	"math"
	"strings"
	"testing"
)

func TestMeanStdev_SingleValue(t *testing.T) {
	mean, sd := meanStdev([]float64{0.8})
	if mean != 0.8 || sd != 0 {
		t.Fatalf("mean/sd=%v/%v want 0.8/0", mean, sd)
	}
}

func TestMeanStdev_MultipleValues(t *testing.T) {
	mean, sd := meanStdev([]float64{1.0, 0.5, 0.0})
	wantMean := 0.5
	wantSD := math.Sqrt(0.25) // sample stdev: sqrt((0.25+0+0.25)/2)
	if math.Abs(mean-wantMean) > 1e-9 {
		t.Fatalf("mean=%v want %v", mean, wantMean)
	}
	if math.Abs(sd-wantSD) > 1e-9 {
		t.Fatalf("sd=%v want %v", sd, wantSD)
	}
}

func TestMeanStdev_Empty(t *testing.T) {
	mean, sd := meanStdev(nil)
	if mean != 0 || sd != 0 {
		t.Fatalf("mean/sd=%v/%v want 0/0", mean, sd)
	}
}

func TestAnyDrawsAboveOne(t *testing.T) {
	if anyDrawsAboveOne([]Cell{{Draw: 1}, {Draw: 1}}) {
		t.Fatalf("expected false when all draws==1")
	}
	if !anyDrawsAboveOne([]Cell{{Draw: 1}, {Draw: 2}}) {
		t.Fatalf("expected true when any draw > 1")
	}
}

func TestWriteTaskSection_StdevColumnAppearsOnlyForMultiDraw(t *testing.T) {
	cells := []Cell{
		{TaskID: "t", FixtureID: "f1", Model: ModelSpec{Label: "m", Provider: ProviderLocal()}, Draw: 1, Score: 0.7, Passes: true, WallMS: 1000},
		{TaskID: "t", FixtureID: "f1", Model: ModelSpec{Label: "m", Provider: ProviderLocal()}, Draw: 2, Score: 0.9, Passes: true, WallMS: 1200},
	}
	var b strings.Builder
	writeTaskSection(&b, cells)
	out := b.String()
	if !strings.Contains(out, "stdev") {
		t.Fatalf("expected stdev column when draws > 1; got:\n%s", out)
	}
	if !strings.Contains(out, "±") {
		t.Fatalf("expected ± marker in row; got:\n%s", out)
	}
}

func TestWriteTaskSection_NoStdevColumnForSingleDraw(t *testing.T) {
	cells := []Cell{
		{TaskID: "t", FixtureID: "f1", Model: ModelSpec{Label: "m", Provider: ProviderLocal()}, Draw: 1, Score: 0.7, Passes: true, WallMS: 1000},
	}
	var b strings.Builder
	writeTaskSection(&b, cells)
	out := b.String()
	if strings.Contains(out, "stdev") {
		t.Fatalf("did not expect stdev column for single-draw run; got:\n%s", out)
	}
}
