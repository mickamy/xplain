package parser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/mickamy/xplain/internal/model"
)

// ParseJSON reads a PostgreSQL EXPLAIN (FORMAT JSON) document and produces an Explain structure.
func ParseJSON(r io.Reader) (*model.Explain, error) {
	decoder := json.NewDecoder(r)
	decoder.UseNumber()

	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode explain json: %w", err)
	}

	entry, err := pickFirstEntry(payload)
	if err != nil {
		return nil, err
	}

	planMapVal, ok := entry["Plan"]
	if !ok {
		return nil, errors.New("explain json: missing Plan root")
	}

	planMap, err := asObject(planMapVal)
	if err != nil {
		return nil, fmt.Errorf("explain json: invalid Plan node: %w", err)
	}

	root, err := parsePlanNode(planMap, "0")
	if err != nil {
		return nil, err
	}

	explain := &model.Explain{
		Plan:          root,
		PlanningTime:  asFloat(entry["Planning Time"]),
		ExecutionTime: asFloat(entry["Execution Time"]),
		Settings:      parseSettings(entry["Settings"]),
		Extra:         map[string]any{},
	}

	for k, v := range entry {
		if k == "Plan" || k == "Planning Time" || k == "Execution Time" || k == "Settings" {
			continue
		}
		explain.Extra[k] = v
	}

	return explain, nil
}

func pickFirstEntry(payload any) (map[string]any, error) {
	switch v := payload.(type) {
	case []any:
		if len(v) == 0 {
			return nil, errors.New("explain json: empty payload")
		}
		obj, err := asObject(v[0])
		if err != nil {
			return nil, fmt.Errorf("explain json: invalid entry: %w", err)
		}
		return obj, nil
	case map[string]any:
		return v, nil
	default:
		return nil, fmt.Errorf("explain json: unexpected top-level type %T", payload)
	}
}

func parsePlanNode(data map[string]any, path string) (*model.PlanNode, error) {
	node := &model.PlanNode{
		ID:                 path,
		NodeType:           asString(data["Node Type"]),
		RelationName:       asString(data["Relation Name"]),
		Schema:             asString(data["Schema"]),
		Alias:              asString(data["Alias"]),
		ParentRelationship: asString(data["Parent Relationship"]),
		StartupCost:        asFloat(data["Startup Cost"]),
		TotalCost:          asFloat(data["Total Cost"]),
		PlanRows:           asFloat(data["Plan Rows"]),
		PlanWidth:          asFloat(data["Plan Width"]),
		ActualStartupTime:  asFloat(data["Actual Startup Time"]),
		ActualTotalTime:    asFloat(data["Actual Total Time"]),
		ActualRows:         asFloat(data["Actual Rows"]),
		ActualLoops:        asFloat(data["Actual Loops"]),
		WorkersPlanned:     asFloat(data["Workers Planned"]),
		WorkersLaunched:    asFloat(data["Workers Launched"]),
		Output:             asStringSlice(data["Output"]),
		Filter:             asString(data["Filter"]),
		JoinType:           asString(data["Join Type"]),
		IndexName:          asString(data["Index Name"]),
		HashCond:           asString(data["Hash Cond"]),
		MergeCond:          asString(data["Merge Cond"]),
		SortKey:            asStringSlice(data["Sort Key"]),
		GroupKey:           asStringSlice(data["Group Key"]),
		Extra:              map[string]any{},
	}

	node.Buffers = parseBuffers(data)

	childrenSlice := asSlice(data["Plans"])

	for i, childVal := range childrenSlice {
		childMap, err := asObject(childVal)
		if err != nil {
			return nil, fmt.Errorf("parse child plan (%s.%d): %w", path, i, err)
		}

		child, err := parsePlanNode(childMap, fmt.Sprintf("%s.%d", path, i))
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, child)
	}

	known := map[string]struct{}{
		"Node Type":             {},
		"Relation Name":         {},
		"Schema":                {},
		"Alias":                 {},
		"Parent Relationship":   {},
		"Startup Cost":          {},
		"Total Cost":            {},
		"Plan Rows":             {},
		"Plan Width":            {},
		"Actual Startup Time":   {},
		"Actual Total Time":     {},
		"Actual Rows":           {},
		"Actual Loops":          {},
		"Workers Planned":       {},
		"Workers Launched":      {},
		"Output":                {},
		"Filter":                {},
		"Join Type":             {},
		"Index Name":            {},
		"Hash Cond":             {},
		"Merge Cond":            {},
		"Sort Key":              {},
		"Group Key":             {},
		"Plans":                 {},
		"Shared Hit Blocks":     {},
		"Shared Read Blocks":    {},
		"Shared Dirtied Blocks": {},
		"Shared Written Blocks": {},
		"Local Hit Blocks":      {},
		"Local Read Blocks":     {},
		"Local Dirtied Blocks":  {},
		"Local Written Blocks":  {},
		"Temp Read Blocks":      {},
		"Temp Written Blocks":   {},
		"I/O Read Time":         {},
		"I/O Write Time":        {},
	}

	for k, v := range data {
		if _, ok := known[k]; ok {
			continue
		}
		node.Extra[k] = v
	}

	return node, nil
}

func parseBuffers(data map[string]any) model.Buffers {
	return model.Buffers{
		SharedHit:       asInt64(data["Shared Hit Blocks"]),
		SharedRead:      asInt64(data["Shared Read Blocks"]),
		SharedDirtied:   asInt64(data["Shared Dirtied Blocks"]),
		SharedWritten:   asInt64(data["Shared Written Blocks"]),
		LocalHit:        asInt64(data["Local Hit Blocks"]),
		LocalRead:       asInt64(data["Local Read Blocks"]),
		LocalDirtied:    asInt64(data["Local Dirtied Blocks"]),
		LocalWritten:    asInt64(data["Local Written Blocks"]),
		TempRead:        asInt64(data["Temp Read Blocks"]),
		TempWritten:     asInt64(data["Temp Written Blocks"]),
		IOReadTimeMs:    asFloat(data["I/O Read Time"]),
		IOWriteTimeMs:   asFloat(data["I/O Write Time"]),
		BlockReadTimeMs: asFloat(data["Block Read Time"]),
	}
}

func parseSettings(val any) map[string]string {
	if val == nil {
		return nil
	}

	result := map[string]string{}
	switch typed := val.(type) {
	case []any:
		for _, entry := range typed {
			item, err := asObject(entry)
			if err != nil {
				continue
			}
			name := asString(item["Name"])
			if name == "" {
				name = asString(item["name"])
			}
			value := asString(item["Setting"])
			if value == "" {
				value = asString(item["value"])
			}
			if name != "" && value != "" {
				result[name] = value
			}
		}
	case map[string]any:
		for k, v := range typed {
			result[k] = fmt.Sprint(v)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func asObject(val any) (map[string]any, error) {
	if val == nil {
		return nil, errors.New("nil object")
	}
	obj, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object, got %T", val)
	}
	return obj, nil
}

func asSlice(val any) []any {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case []any:
		return v
	default:
		return nil
	}
}

func asString(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func asStringSlice(val any) []string {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, asString(item))
		}
		return out
	case []string:
		return append([]string(nil), v...)
	case string:
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		return out
	default:
		return nil
	}
}

func asFloat(val any) float64 {
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0
		}
		return f
	case string:
		if v == "" {
			return 0
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

func asInt64(val any) int64 {
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(math.Round(v))
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return i
		}
		f, err := v.Float64()
		if err != nil {
			return 0
		}
		return int64(math.Round(f))
	case string:
		if v == "" {
			return 0
		}
		if strings.ContainsRune(v, '.') {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return 0
			}
			return int64(math.Round(f))
		}
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0
		}
		return i
	default:
		return 0
	}
}
