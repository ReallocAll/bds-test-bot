package scenario

import (
	"encoding/json"
	"fmt"
)

// Step describes one action invocation in a scenario.
type Step struct {
	Action string `json:"action"`
	Ticks  int    `json:"ticks,omitempty"`
	Repeat int    `json:"repeat,omitempty"`
}

// Scenario is a deterministic action sequence loaded from JSON.
type Scenario struct {
	Name  string `json:"name"`
	Steps []Step `json:"steps"`
}

func Parse(data []byte) (Scenario, error) {
	var s Scenario
	if err := json.Unmarshal(data, &s); err != nil {
		return Scenario{}, err
	}
	if s.Name == "" {
		return Scenario{}, fmt.Errorf("scenario name is required")
	}
	for i, step := range s.Steps {
		if step.Action == "" {
			return Scenario{}, fmt.Errorf("step %d action is required", i)
		}
	}
	return s, nil
}
