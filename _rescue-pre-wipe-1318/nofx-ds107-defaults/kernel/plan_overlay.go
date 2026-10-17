package kernel

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// P5.1 — a self-contained RFC-6902 (JSON Patch) applier over RFC-6901 pointers.
// No external lib exists; the plan grammar only needs add/remove/replace/test on
// shallow paths (/bias/direction, /levels/-, /levels/N, /scenarios/N/..., /no_trade/-,
// /death_condition). The `test` op is the concurrency guard (§42 "test-op
// concurrency"): a write is rejected if the base changed under it.

// PatchOp is one RFC-6902 operation.
type PatchOp struct {
	Op    string          `json:"op"`   // add | remove | replace | test
	Path  string          `json:"path"` // RFC-6901 JSON Pointer
	Value json.RawMessage `json:"value,omitempty"`
	From  string          `json:"from,omitempty"` // move/copy — unsupported (rejected)
}

// ApplyPatchStrict applies ONE patch (a JSON array of ops) to doc atomically:
// any op error (bad path, failed `test`, unsupported op) aborts with NOTHING
// applied. Used at WRITE time (POST /plan/overlay) as the concurrency + validity
// guard, and as the unit each read-time overlay is tried through.
func ApplyPatchStrict(doc []byte, patchJSON string) ([]byte, error) {
	var ops []PatchOp
	if err := json.Unmarshal([]byte(patchJSON), &ops); err != nil {
		return nil, fmt.Errorf("patch is not a JSON op array: %w", err)
	}
	var root interface{}
	if err := json.Unmarshal(doc, &root); err != nil {
		return nil, fmt.Errorf("base doc invalid JSON: %w", err)
	}
	for i, op := range ops {
		next, err := applyOp(root, op)
		if err != nil {
			return nil, fmt.Errorf("op[%d] %s %q: %w", i, op.Op, op.Path, err)
		}
		root = next
	}
	out, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ApplyOverlayPatches folds each overlay patch (ASC order) into baseDoc, producing
// plan_final. A patch that fails to apply (bad op / failed test) is SKIPPED — a
// single bad overlay never corrupts the resolved plan; its error is collected.
// Read-time robustness (the card must always render something coherent).
func ApplyOverlayPatches(baseDoc []byte, patches []string) ([]byte, []error) {
	cur := baseDoc
	var errs []error
	for i, p := range patches {
		next, err := ApplyPatchStrict(cur, p)
		if err != nil {
			errs = append(errs, fmt.Errorf("overlay[%d]: %w", i, err))
			continue // skip this overlay, keep the last-good doc
		}
		cur = next
	}
	return cur, errs
}

// applyOp dispatches one op, returning the (possibly new) root.
func applyOp(root interface{}, op PatchOp) (interface{}, error) {
	switch op.Op {
	case "add", "replace", "remove", "test":
	default:
		return nil, fmt.Errorf("unsupported op %q (only add/remove/replace/test)", op.Op)
	}
	tokens, err := parsePointer(op.Path)
	if err != nil {
		return nil, err
	}
	// Whole-document ops (path "").
	if len(tokens) == 0 {
		switch op.Op {
		case "replace", "add":
			return decodeValue(op.Value)
		case "test":
			v, err := decodeValue(op.Value)
			if err != nil {
				return nil, err
			}
			if !reflect.DeepEqual(root, v) {
				return nil, errTestFailed
			}
			return root, nil
		default:
			return nil, fmt.Errorf("cannot %s the whole document", op.Op)
		}
	}
	return modify(root, tokens, op)
}

var errTestFailed = fmt.Errorf("test op failed (base changed)")

// modify descends to the parent of the final token, applies the leaf op, and
// rebuilds the node chain upward (so slice appends/removes propagate).
func modify(node interface{}, tokens []string, op PatchOp) (interface{}, error) {
	if len(tokens) == 1 {
		return applyLeaf(node, tokens[0], op)
	}
	key := tokens[0]
	switch c := node.(type) {
	case map[string]interface{}:
		child, ok := c[key]
		if !ok {
			return nil, fmt.Errorf("path segment %q not found", key)
		}
		nc, err := modify(child, tokens[1:], op)
		if err != nil {
			return nil, err
		}
		c[key] = nc
		return c, nil
	case []interface{}:
		i, err := arrayIndex(key, len(c), false)
		if err != nil {
			return nil, err
		}
		nc, err := modify(c[i], tokens[1:], op)
		if err != nil {
			return nil, err
		}
		c[i] = nc
		return c, nil
	default:
		return nil, fmt.Errorf("cannot descend into %q (not object/array)", key)
	}
}

// applyLeaf performs the op on `parent` at the final `token`.
func applyLeaf(parent interface{}, token string, op PatchOp) (interface{}, error) {
	switch c := parent.(type) {
	case map[string]interface{}:
		switch op.Op {
		case "add", "replace":
			if op.Op == "replace" {
				if _, ok := c[token]; !ok {
					return nil, fmt.Errorf("replace target %q missing", token)
				}
			}
			v, err := decodeValue(op.Value)
			if err != nil {
				return nil, err
			}
			c[token] = v
			return c, nil
		case "remove":
			if _, ok := c[token]; !ok {
				return nil, fmt.Errorf("remove target %q missing", token)
			}
			delete(c, token)
			return c, nil
		case "test":
			cur, ok := c[token]
			if !ok {
				return nil, fmt.Errorf("test target %q missing", token)
			}
			return c, testEqual(cur, op.Value)
		}
	case []interface{}:
		switch op.Op {
		case "add":
			v, err := decodeValue(op.Value)
			if err != nil {
				return nil, err
			}
			if token == "-" {
				return append(c, v), nil
			}
			i, err := arrayIndex(token, len(c), true) // add allows index == len
			if err != nil {
				return nil, err
			}
			out := make([]interface{}, 0, len(c)+1)
			out = append(out, c[:i]...)
			out = append(out, v)
			out = append(out, c[i:]...)
			return out, nil
		case "replace":
			i, err := arrayIndex(token, len(c), false)
			if err != nil {
				return nil, err
			}
			v, err := decodeValue(op.Value)
			if err != nil {
				return nil, err
			}
			c[i] = v
			return c, nil
		case "remove":
			i, err := arrayIndex(token, len(c), false)
			if err != nil {
				return nil, err
			}
			return append(c[:i], c[i+1:]...), nil
		case "test":
			i, err := arrayIndex(token, len(c), false)
			if err != nil {
				return nil, err
			}
			return c, testEqual(c[i], op.Value)
		}
	}
	return nil, fmt.Errorf("cannot apply %s at %q (parent not object/array)", op.Op, token)
}

// testEqual compares the current value against the op value (both JSON-decoded).
func testEqual(cur interface{}, raw json.RawMessage) error {
	want, err := decodeValue(raw)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(cur, want) {
		return errTestFailed
	}
	return nil
}

func decodeValue(raw json.RawMessage) (interface{}, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("op requires a value")
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("bad value JSON: %w", err)
	}
	return v, nil
}

// parsePointer splits an RFC-6901 pointer into unescaped tokens ("" → no tokens).
func parsePointer(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("pointer must start with '/'")
	}
	parts := strings.Split(p[1:], "/")
	for i, seg := range parts {
		seg = strings.ReplaceAll(seg, "~1", "/")
		seg = strings.ReplaceAll(seg, "~0", "~")
		parts[i] = seg
	}
	return parts, nil
}

// arrayIndex parses a numeric array token. allowAppend permits index == length
// (RFC-6902 add semantics). Rejects negatives, leading zeros, and out-of-range.
func arrayIndex(token string, length int, allowAppend bool) (int, error) {
	// RFC-6901 canonical form only: no empty, no sign ("-0"/"-1"), no leading zero.
	if token == "" || token[0] == '-' || (len(token) > 1 && token[0] == '0') {
		return 0, fmt.Errorf("invalid array index %q", token)
	}
	i, err := strconv.Atoi(token)
	if err != nil || i < 0 {
		return 0, fmt.Errorf("invalid array index %q", token)
	}
	limit := length
	if !allowAppend {
		limit = length - 1
	}
	if i > limit {
		return 0, fmt.Errorf("array index %d out of range (len %d)", i, length)
	}
	return i, nil
}
