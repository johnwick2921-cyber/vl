package main

// Command audit0926fields walks the StrategyConfig struct graph with the Go
// parser and prints every field as JSON — the census rows' defined_at/type
// spine, including json:"-" compatibility fields (remapped to their real
// ai_config paths, mirroring store/strategy.go MarshalJSON).
//
// READ-ONLY: parses source, prints JSON. Run: go run ./scripts/audit0926fields

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fieldRow struct {
	GoPath    string `json:"go_path"`
	JSONPath  string `json:"json_path"`
	DefinedAt string `json:"defined_at"`
	Type      string `json:"type"`
	Container bool   `json:"container"`
	Compat    bool   `json:"compat"` // json:"-" field re-emitted by MarshalJSON
}

var (
	// compatJSON maps the five json:"-" compatibility fields to their real
	// output paths (store/strategy.go MarshalJSON nests them under ai_config).
	compatJSON = map[string]string{
		"CoinSource":     "ai_config.coin_source",
		"Indicators":     "ai_config.indicators",
		"CustomPrompt":   "ai_config.custom_prompt",
		"RiskControl":    "ai_config.risk_control",
		"PromptSections": "ai_config.prompt_sections",
	}
	noTags bool
)

func main() {
	storeDir := "store"
	rootType := "StrategyConfig"
	if len(os.Args) > 1 {
		rootType = os.Args[1]
	}
	if len(os.Args) > 2 && os.Args[2] == "notags" {
		noTags = true
	}
	fset := token.NewFileSet()
	structs := map[string]*ast.StructType{}
	fileOf := map[string]string{}

	var files []string
	for _, dir := range []string{storeDir, "config"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
	}
	for _, f := range files {
		tree, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse", f, ":", err)
			os.Exit(1)
		}
		for name, obj := range tree.Scope.Objects {
			if obj.Kind == ast.Typ {
				if ts, ok := obj.Decl.(*ast.TypeSpec); ok {
					if st, ok := ts.Type.(*ast.StructType); ok {
						structs[name] = st
						fileOf[name] = f
					}
				}
			}
		}
	}

	var rows []fieldRow
	seen := map[string]bool{}
	var walk func(structName, goPrefix, jsonPrefix string, st *ast.StructType, depth int)
	walk = func(structName, goPrefix, jsonPrefix string, st *ast.StructType, depth int) {
		if depth > 10 || st == nil {
			return
		}
		for _, f := range st.Fields.List {
			if len(f.Names) == 0 {
				continue // embedded — treat as container, no JSON path of its own
			}
			name := f.Names[0].Name
			goPath := name
			if goPrefix != "" {
				goPath = goPrefix + "." + name
			}
			tag := ""
			if f.Tag != nil {
				tag = strings.Trim(f.Tag.Value, "`")
			}
			jsonName := ""
			compat := false
			for _, part := range strings.Fields(tag) {
				if strings.HasPrefix(part, "json:") {
					jsonName = strings.Trim(part[5:], `"`)
					if i := strings.Index(jsonName, ","); i >= 0 {
						jsonName = jsonName[:i]
					}
					break
				}
			}
			if jsonName == "-" {
				compat = true
				jsonName = compatJSON[name]
			}
			if jsonName == "" {
				if noTags {
					jsonName = name
				} else {
					// no json tag at all — skip (not a schema field)
					continue
				}
			}
			jsonPath := jsonName
			if jsonPrefix != "" && !compat {
				jsonPath = jsonPrefix + "." + jsonName
			}
			if seen[goPath] {
				continue
			}
			seen[goPath] = true

			typeStr := exprString(f.Type)
			row := fieldRow{
				GoPath:    goPath,
				JSONPath:  jsonPath,
				DefinedAt: fmt.Sprintf("%s:%d", fileOf[structName], fset.Position(f.Pos()).Line),
				Type:      typeStr,
				Compat:    compat,
			}
			// resolve nested struct type name
			inner := typeName(f.Type)
			if child, ok := structs[inner]; ok {
				row.Container = true
				rows = append(rows, row)
				walk(inner, goPath, jsonPath, child, depth+1)
				continue
			}
			// slice of structs / map values are containers by shape, not by name
			if strings.HasPrefix(typeStr, "[]") {
				if el := strings.TrimPrefix(typeStr, "[]"); el != "" {
					if cs, ok := structs[el]; ok {
						row.Container = true
						rows = append(rows, row)
						walk(el, goPath, jsonPath, cs, depth+1)
						continue
					}
				}
			}
			rows = append(rows, row)
		}
	}
	var roots []string
	roots = append(roots, rootType)
	if rootType == "StrategyConfig" {
		roots = append(roots, "rawStrategyConfig")
	}
	for _, rt := range roots {
		walk(rt, "", "", structs[rt], 0)
	}

	byPath := map[string]fieldRow{}
	for _, r := range rows {
		if prev, ok := byPath[r.JSONPath]; ok {
			// keep the canonical root's definition; the raw mirror's
			// DefinedAt is recorded in the container-less duplicate only when
			// the path is NEW. Prefer the first root when both exist.
			if rootType == "StrategyConfig" && strings.HasPrefix(r.GoPath, "rawStrategyConfig.") {
				continue
			}
			_ = prev
		}
		byPath[r.JSONPath] = r
	}
	rows = rows[:0]
	for _, r := range byPath {
		rows = append(rows, r)
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].JSONPath < rows[j].JSONPath })

	b, err := json.MarshalIndent(rows, "", " ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		os.Exit(1)
	}
	fmt.Println(string(b))
}

func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeName(t.X)
	case *ast.ArrayType:
		return typeName(t.Elt)
	case *ast.MapType:
		return typeName(t.Value)
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.ArrayType:
		return "[]" + exprString(t.Elt)
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *ast.SelectorExpr:
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.BasicLit:
		return t.Value
	}
	return fmt.Sprintf("%T", e)
}
