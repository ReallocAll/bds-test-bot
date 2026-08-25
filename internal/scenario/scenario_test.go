package scenario

import "testing"

func TestParseScenario(t *testing.T) {
	s, err := Parse([]byte(`{"name":"walk","steps":[{"action":"move","ticks":20}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "walk" || len(s.Steps) != 1 || s.Steps[0].Action != "move" {
		t.Fatalf("unexpected scenario: %+v", s)
	}
}

func TestParseRejectsMissingAction(t *testing.T) {
	if _, err := Parse([]byte(`{"name":"bad","steps":[{}]}`)); err == nil {
		t.Fatal("expected error")
	}
}
