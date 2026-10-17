// Package appctx assembles the dashboard context — Go-native port of the Deno
// context.ts (build_context, discover_projects, resolve_project_cwd, build_data).
//
// The Deno original navigates plain JS objects (Record<string, unknown>)
// uniformly. To port build_data faithfully, the context is normalized to a fully
// dynamic tree (map[string]any / []any / float64 / string / bool) at the
// build_data boundary via a JSON round-trip — so the dynamic accessors below
// behave exactly like JS property access / String() / Number() / Boolean().
package appctx

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
)

// dyn JSON round-trips a value into a fully dynamic tree.
func dyn(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

func aMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func aArr(v any) []any          { a, _ := v.([]any); return a }

// aStr mirrors JS `String(x ?? "")`: null/undefined → "".
func aStr(v any) string {
	if v == nil {
		return ""
	}
	return jsString(v)
}

func jsString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// aNum mirrors JS `Number(x ?? 0)`. Handles Go-native numeric types too: values
// built AFTER the dyn round-trip (e.g. collectors.CtxTok → int) are not float64,
// so int/int64 must be accepted or sums silently drop them.
func aNum(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

// aBool mirrors JS Boolean(x) truthiness.
func aBool(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0 && !math.IsNaN(x)
	case float32:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	case int32:
		return x != 0
	default:
		return true // objects/arrays are truthy
	}
}

// nc mirrors JS `x ?? def` for strings: null/undefined → def, else String(x).
func nc(v any, def string) string {
	if v == nil {
		return def
	}
	return jsString(v)
}

// ncChain mirrors `a ?? b ?? c`: the first non-nil value, String()-ified.
func ncChain(vals ...any) string {
	for _, v := range vals {
		if v != nil {
			return jsString(v)
		}
	}
	return ""
}

// sumField sums aNum(item[key]) over a slice of dynamic objects.
func sumField(items []map[string]any, key string) float64 {
	var sum float64
	for _, it := range items {
		sum += aNum(it[key])
	}
	return sum
}

// orEmptyArr mirrors `(x as unknown[]) ?? []`: x if it is an array, else [].
func orEmptyArr(v any) any {
	if a, ok := v.([]any); ok {
		return a
	}
	return []any{}
}

// sortedKeys returns a map's keys sorted ascending (deterministic iteration).
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// numOr0 mirrors `Number(x) || 0`.
func numOr0(v any) float64 { return aNum(v) }

func itoa(n int) string { return strconv.Itoa(n) }

// dashIf returns "—" when cond is true, else val.
func dashIf(cond bool, val string) string {
	if cond {
		return "—"
	}
	return val
}

// lastCost mirrors `last.cost ?? 0` (0 when last/cost is absent).
func lastCost(last map[string]any) any {
	if last == nil {
		return float64(0)
	}
	if c := last["cost"]; c != nil {
		return c
	}
	return float64(0)
}
