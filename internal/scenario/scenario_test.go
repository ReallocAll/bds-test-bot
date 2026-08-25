package scenario

import "testing"

func TestParseJSONScenario(t *testing.T) {
	s, err := Parse([]byte(`{"name":"walk","steps":[{"action":"move","ticks":20,"forward":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "walk" || len(s.Steps) != 1 || s.Steps[0].Action != ActionMove {
		t.Fatalf("unexpected scenario: %+v", s)
	}
}

func TestParseYAMLScenario(t *testing.T) {
	s, err := Parse([]byte("name: mixed\nsteps:\n  - action: wait\n    ticks: 5\n  - action: jump\n    ticks: 2\n    repeat: 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "mixed" || len(s.Steps) != 2 || s.Steps[1].Repeat != 3 {
		t.Fatalf("unexpected scenario: %+v", s)
	}
}

func TestParseRejectsMissingAction(t *testing.T) {
	if _, err := Parse([]byte(`{"name":"bad","steps":[{}]}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseRejectsInvalidMove(t *testing.T) {
	if _, err := Parse([]byte(`{"name":"bad","steps":[{"action":"move","ticks":10}]}`)); err == nil {
		t.Fatal("expected move vector validation error")
	}
}
