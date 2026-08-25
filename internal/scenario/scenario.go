package scenario

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ActionIdle    = "idle"
	ActionWait    = "wait"
	ActionMove    = "move"
	ActionLook    = "look"
	ActionJump    = "jump"
	ActionChat    = "chat"
	ActionCommand = "command"
	ActionSwing   = "swing"
)

// Step describes one action invocation in a scenario.
type Step struct {
	Action  string   `json:"action" yaml:"action"`
	Ticks   int      `json:"ticks,omitempty" yaml:"ticks,omitempty"`
	Repeat  int      `json:"repeat,omitempty" yaml:"repeat,omitempty"`
	Forward float32  `json:"forward,omitempty" yaml:"forward,omitempty"`
	Strafe  float32  `json:"strafe,omitempty" yaml:"strafe,omitempty"`
	Speed   float32  `json:"speed,omitempty" yaml:"speed,omitempty"`
	Yaw     *float32 `json:"yaw,omitempty" yaml:"yaw,omitempty"`
	Pitch   *float32 `json:"pitch,omitempty" yaml:"pitch,omitempty"`
	Message string   `json:"message,omitempty" yaml:"message,omitempty"`
	Command string   `json:"command,omitempty" yaml:"command,omitempty"`
}

// Scenario is a deterministic action sequence loaded from JSON or YAML.
type Scenario struct {
	Name  string `json:"name" yaml:"name"`
	Steps []Step `json:"steps" yaml:"steps"`
}

// Parse accepts JSON or YAML scenario data.
func Parse(data []byte) (Scenario, error) {
	var s Scenario
	if err := json.Unmarshal(data, &s); err != nil {
		if yamlErr := yaml.Unmarshal(data, &s); yamlErr != nil {
			return Scenario{}, fmt.Errorf("parse scenario as JSON: %v; as YAML: %w", err, yamlErr)
		}
	}
	if err := s.Validate(); err != nil {
		return Scenario{}, err
	}
	return s, nil
}

// Load reads and parses a scenario definition from disk.
func Load(path string) (Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("read scenario file %q: %w", path, err)
	}
	return Parse(data)
}

func (s Scenario) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("scenario name is required")
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("scenario must contain at least one step")
	}
	for i, step := range s.Steps {
		if err := validateStep(i, step); err != nil {
			return err
		}
	}
	return nil
}

func validateStep(index int, step Step) error {
	if step.Action == "" {
		return fmt.Errorf("step %d action is required", index)
	}
	if step.Ticks < 0 {
		return fmt.Errorf("step %d ticks must not be negative", index)
	}
	if step.Repeat < 0 {
		return fmt.Errorf("step %d repeat must not be negative", index)
	}
	if step.Forward < -1 || step.Forward > 1 || step.Strafe < -1 || step.Strafe > 1 {
		return fmt.Errorf("step %d movement vector components must be in range -1..1", index)
	}
	if step.Speed < 0 {
		return fmt.Errorf("step %d speed must not be negative", index)
	}

	switch step.Action {
	case ActionIdle:
	case ActionWait:
		if step.Ticks <= 0 {
			return fmt.Errorf("step %d wait requires ticks > 0", index)
		}
	case ActionMove:
		if step.Forward == 0 && step.Strafe == 0 {
			return fmt.Errorf("step %d move requires forward or strafe input", index)
		}
	case ActionLook:
		if step.Yaw == nil && step.Pitch == nil {
			return fmt.Errorf("step %d look requires yaw or pitch", index)
		}
		if step.Ticks <= 0 {
			return fmt.Errorf("step %d look requires ticks > 0", index)
		}
	case ActionJump:
		if step.Ticks <= 0 {
			return fmt.Errorf("step %d jump requires ticks > 0", index)
		}
	case ActionChat:
		if strings.TrimSpace(step.Message) == "" {
			return fmt.Errorf("step %d chat requires message", index)
		}
	case ActionCommand:
		if strings.TrimSpace(step.Command) == "" {
			return fmt.Errorf("step %d command requires command", index)
		}
	case ActionSwing:
	default:
		return fmt.Errorf("step %d unsupported action %q", index, step.Action)
	}
	return nil
}
