package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteActivityLogCreatesLinkedDailyLog(t *testing.T) {
	cfg, root := newSleepVault(t)
	res, err := WriteActivityLog(cfg, ActivityLogOpts{
		Sphere:    SphereWork,
		Date:      "2026-05-06",
		Operation: "phase4.apply",
		Tool:      "sloptools",
		Message:   "promoted Georg alias cleanup",
		Links: []string{
			"brain/people/Georg Grassler.md",
			filepath.Join(root, "brain", "institutions", "TU Graz.md"),
			"../outside.md",
		},
		Now: time.Date(2026, 5, 6, 12, 15, 0, 0, time.Local),
	})
	if err != nil {
		t.Fatalf("WriteActivityLog: %v", err)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read activity log: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "kind: activity_log") {
		t.Fatalf("frontmatter missing:\n%s", body)
	}
	if !strings.Contains(body, "12:15 sloptools/phase4.apply: promoted Georg alias cleanup") {
		t.Fatalf("entry missing:\n%s", body)
	}
	if !strings.Contains(body, "[[people/Georg Grassler]]") || !strings.Contains(body, "[[institutions/TU Graz]]") {
		t.Fatalf("links missing:\n%s", body)
	}
	if strings.Contains(body, "../outside") {
		t.Fatalf("outside link leaked:\n%s", body)
	}
}

func TestRunSleepIncludesActivityLogWhenRequested(t *testing.T) {
	cfg, _ := newSleepVault(t)
	_, err := WriteActivityLog(cfg, ActivityLogOpts{
		Sphere:  SphereWork,
		Date:    "2026-05-06",
		Message: "reviewed [[topics/KINEQ]]",
		Links:   []string{"topics/KINEQ.md"},
		Now:     time.Date(2026, 5, 6, 8, 0, 0, 0, time.Local),
	})
	if err != nil {
		t.Fatalf("WriteActivityLog: %v", err)
	}
	res, err := RunSleep(cfg, SleepOpts{
		Sphere:   SphereWork,
		Budget:   4,
		Backend:  SleepBackendNone,
		Activity: true,
		Now:      time.Date(2026, 5, 6, 23, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RunSleep: %v", err)
	}
	if !res.ActivityUsed {
		t.Fatalf("ActivityUsed=false, want true")
	}
	data, err := os.ReadFile(res.ReportPath)
	if err != nil {
		t.Fatalf("read sleep report: %v", err)
	}
	if !strings.Contains(string(data), "## Activity focus") || !strings.Contains(string(data), "[[topics/KINEQ]]") {
		t.Fatalf("sleep report missing activity focus:\n%s", string(data))
	}
}
