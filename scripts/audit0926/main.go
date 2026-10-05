package main
// Command audit0926 dumps the settings census seeds as JSON for the audit CSV:
//   - schema paths via the production marshaller (store.EnumerateSchemaKnobs)
//   - flattened code defaults via store.GetDefaultStrategyConfig("en") (masked)
//   - the knob registry (store.AllKnobs)
//   - the Strategy row columns (hardcoded, mirrors store/strategy.go:699)
//
// READ-ONLY: it only marshals and prints. Run: go run ./scripts/audit0926

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"nofx/store"
)

func flatten(prefix string, v any, out map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			flatten(p, child, out)
		}
	case []any:
		if prefix != "" {
			out[prefix] = v
		}
	default:
		if prefix != "" {
			out[prefix] = v
		}
	}
}

func main() {
	out := map[string]any{}

	paths := store.EnumerateSchemaKnobs()
	sort.Strings(paths)
	out["schema_paths"] = paths
	if err := store.SchemaEnumerationErr(); err != nil {
		out["schema_err"] = err.Error()
	}

	// Defaults through the PRODUCTION marshaller, then flattened.
	def := store.GetDefaultStrategyConfig("en")
	defB, err := json.Marshal(def)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal defaults: %v\n", err)
		os.Exit(1)
	}
	var defTree any
	_ = json.Unmarshal(defB, &defTree)
	defFlat := map[string]any{}
	flatten("", defTree, defFlat)
	out["defaults"] = defFlat

	reg := map[string]any{}
	regAll := []map[string]any{}
	for _, e := range store.AllKnobs() {
		reg[e.Path] = map[string]any{
			"status":    string(e.Status),
			"consumers": e.Consumers,
			"note":      e.Note,
		}
		regAll = append(regAll, map[string]any{
			"path":      e.Path,
			"status":    string(e.Status),
			"consumers": e.Consumers,
			"dual":      e.DualLevel,
			"clamp":     e.Clamp,
			"note":      e.Note,
		})
	}
	out["registry"] = reg
	out["registry_all"] = regAll

	out["strategy_row_columns"] = []string{
		"id", "user_id", "name", "description", "is_active", "is_default",
		"is_public", "config_visible", "config", "created_at", "updated_at",
	}

	b, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal out: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(b))
}
