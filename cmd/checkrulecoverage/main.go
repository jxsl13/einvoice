// Command checkrulecoverage verifies that every predicate in an enabled
// inventory capability is referenced by production validation code.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type inventory struct {
	Capabilities []struct {
		Group     string `json:"group"`
		Supported bool   `json:"supported"`
	} `json:"capabilities"`
	Syntaxes []struct {
		Patterns []struct {
			Group string `json:"group"`
			Rules []struct {
				ID string `json:"id"`
			} `json:"rules"`
		} `json:"patterns"`
	} `json:"syntaxes"`
}

func main() {
	path := flag.String("inventory", "conformance/xrechnung-3.0.2-rules.json", "rule inventory path")
	flag.Parse()
	if err := run(*path); err != nil {
		fmt.Fprintln(os.Stderr, "checkrulecoverage:", err)
		os.Exit(1)
	}
}

func run(inventoryPath string) error {
	data, err := os.ReadFile(inventoryPath)
	if err != nil {
		return err
	}
	var document inventory
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	enabled := make(map[string]bool, len(document.Capabilities))
	for _, capability := range document.Capabilities {
		enabled[capability.Group] = capability.Supported
	}
	definitions, err := ruleDefinitions("rules")
	if err != nil {
		return err
	}
	used, err := referencedRules(definitions, ".", "validator")
	if err != nil {
		return err
	}
	missingSet := make(map[string]struct{})
	checked := make(map[string]struct{})
	for _, syntax := range document.Syntaxes {
		for _, pattern := range syntax.Patterns {
			if !enabled[pattern.Group] {
				continue
			}
			for _, rule := range pattern.Rules {
				checked[rule.ID] = struct{}{}
				if !used[rule.ID] {
					missingSet[rule.ID] = struct{}{}
				}
			}
		}
	}
	if len(checked) == 0 {
		return errors.New("enabled inventory contains no predicates")
	}
	missing := make([]string, 0, len(missingSet))
	for id := range missingSet {
		missing = append(missing, id)
	}
	slices.Sort(missing)
	if len(missing) != 0 {
		return fmt.Errorf("%d enabled predicates lack runtime references: %s", len(missing), strings.Join(missing, ", "))
	}
	fmt.Printf("verified %d enabled predicate IDs with runtime references\n", len(checked))
	return nil
}

func ruleDefinitions(directory string) (map[string]string, error) {
	definitions := make(map[string]string)
	err := parseGoFiles(directory, func(file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			declaration, ok := node.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for index, value := range declaration.Values {
				if index >= len(declaration.Names) {
					break
				}
				literal, ok := value.(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, element := range literal.Elts {
					field, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					name, ok := field.Key.(*ast.Ident)
					value, valueOK := field.Value.(*ast.BasicLit)
					if !ok || !valueOK || name.Name != "Code" || value.Kind != token.STRING {
						continue
					}
					code, err := strconv.Unquote(value.Value)
					if err == nil {
						definitions[declaration.Names[index].Name] = code
					}
				}
			}
			return true
		})
	})
	return definitions, err
}

func referencedRules(definitions map[string]string, directories ...string) (map[string]bool, error) {
	used := make(map[string]bool)
	for _, directory := range directories {
		err := parseGoFiles(directory, func(file *ast.File) {
			ast.Inspect(file, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				packageName, ok := selector.X.(*ast.Ident)
				if !ok || packageName.Name != "rules" {
					return true
				}
				if code, found := definitions[selector.Sel.Name]; found {
					used[code] = true
				}
				return true
			})
		})
		if err != nil {
			return nil, err
		}
	}
	return used, nil
}

func parseGoFiles(directory string, visit func(*ast.File)) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	files := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, filepath.Join(directory, entry.Name()), nil, 0)
		if err != nil {
			return err
		}
		visit(file)
	}
	return nil
}
