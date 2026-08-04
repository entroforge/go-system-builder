package e2ecoverage

import (
	"encoding/json"
	"fmt"
	"os"
)

// Inventory is the machine-readable E2E scenario inventory (E2E-039).
type Inventory struct {
	Version            string             `json:"version"`
	REQ                string             `json:"req"`
	Updated            string             `json:"updated"`
	Methodology        string             `json:"methodology"`
	Weights            map[string]float64 `json:"weights"`
	AggregatesSnapshot map[string]any     `json:"aggregates_snapshot"`
	Scenarios          []Scenario         `json:"scenarios"`
}

// Scenario records fidelity metadata for one CT/AC/closing/Hook event.
type Scenario struct {
	ID       string   `json:"id"`
	Set      string   `json:"set"`
	Fidelity string   `json:"fidelity"`
	Driver   string   `json:"driver"`
	Seed     string   `json:"seed"`
	TestRefs []string `json:"test_refs"`
	Notes    string   `json:"notes"`
}

// LoadInventory reads and decodes an inventory JSON file.
func LoadInventory(path string) (Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Inventory{}, fmt.Errorf("read inventory: %w", err)
	}
	var inv Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return Inventory{}, fmt.Errorf("decode inventory: %w", err)
	}
	if len(inv.Scenarios) == 0 {
		return Inventory{}, fmt.Errorf("inventory has no scenarios")
	}
	if len(inv.Weights) == 0 {
		inv.Weights = defaultWeights()
	}
	return inv, nil
}

func defaultWeights() map[string]float64 {
	return map[string]float64{
		"L0": 0.0,
		"L1": 0.15,
		"L2": 0.40,
		"L3": 0.70,
		"L4": 1.0,
	}
}
