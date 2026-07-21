package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

func main() {
	os.Exit(runnableMain())
}

func runnableMain() int {
	typesOnly := flag.Bool("types-only", false, "output only type names, one per line")
	format := flag.String("format", "text", "output format: text or json")
	snapshot := flag.Bool("snapshot", false, "show all schema fields for used types, not just selected ones")
	flag.Parse()

	schemaBytes, err := os.ReadFile("graphql/schema/schema.gql")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error reading schema:", err)
		return 1
	}
	schema, gqlErr := gqlparser.LoadSchema(&ast.Source{Name: "schema.gql", Input: string(schemaBytes)})
	if gqlErr != nil {
		fmt.Fprintln(os.Stderr, "error parsing schema:", gqlErr)
		return 1
	}

	files, err := filepath.Glob("graphql/operations/*.graphql")
	if err != nil || len(files) == 0 {
		fmt.Fprintln(os.Stderr, "no operation files found")
		return 1
	}
	var parts []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error reading", f, ":", err)
			return 1
		}
		parts = append(parts, string(b))
	}
	combined := strings.Join(parts, "\n")
	doc, gqlErrList := gqlparser.LoadQuery(schema, combined)
	if gqlErrList != nil {
		for _, e := range gqlErrList {
			fmt.Fprintln(os.Stderr, e.Error())
		}
		return 1
	}

	// field maps: type name -> (field name -> field type string)
	inputFields := map[string]map[string]string{}
	outputFields := map[string]map[string]string{}
	inputUsedIn := map[string]map[string]bool{}
	outputUsedIn := map[string]map[string]bool{}

	rootTypeNames := map[string]bool{}
	if schema.Query != nil {
		rootTypeNames[schema.Query.Name] = true
	}
	if schema.Mutation != nil {
		rootTypeNames[schema.Mutation.Name] = true
	}
	if schema.Subscription != nil {
		rootTypeNames[schema.Subscription.Name] = true
	}

	for _, op := range doc.Operations {
		opName := op.Name
		inputVisited := map[string]bool{}
		for _, v := range op.VariableDefinitions {
			collectInputType(schema, v.Type.Name(), opName, inputFields, inputUsedIn, inputVisited)
		}

		var root *ast.Definition
		switch op.Operation {
		case ast.Query:
			root = schema.Query
		case ast.Mutation:
			root = schema.Mutation
		case ast.Subscription:
			root = schema.Subscription
		}
		if root != nil {
			outputVisited := map[string]bool{}
			collectOutputTypes(schema, doc, op.SelectionSet, root, opName, rootTypeNames, outputFields, outputUsedIn, outputVisited)
		}
	}

	if *snapshot {
		for typeName := range outputFields {
			def := schema.Types[typeName]
			if def == nil {
				continue
			}
			all := map[string]string{}
			for _, f := range def.Fields {
				all[f.Name] = f.Type.String()
			}
			outputFields[typeName] = all
		}
	}

	if *typesOnly {
		seen := map[string]bool{}
		var names []string
		for t := range inputFields {
			if !seen[t] {
				seen[t] = true
				names = append(names, t)
			}
		}
		for t := range outputFields {
			if !seen[t] {
				seen[t] = true
				names = append(names, t)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Println(n)
		}
		return 0
	}

	if *format == "json" {
		return printJSON(inputFields, outputFields, inputUsedIn, outputUsedIn)
	}

	printSection("input", inputFields, inputUsedIn)
	printSection("output", outputFields, outputUsedIn)
	return 0
}

type jsonFieldEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type jsonTypeEntry struct {
	Fields []jsonFieldEntry `json:"fields"`
	UsedIn []string         `json:"used_in"`
}

type jsonOutput struct {
	Input  map[string]jsonTypeEntry `json:"input"`
	Output map[string]jsonTypeEntry `json:"output"`
}

func printJSON(inputFields, outputFields map[string]map[string]string, inputUsedIn, outputUsedIn map[string]map[string]bool) int {
	out := jsonOutput{
		Input:  map[string]jsonTypeEntry{},
		Output: map[string]jsonTypeEntry{},
	}
	for t, fields := range inputFields {
		out.Input[t] = jsonTypeEntry{
			Fields: sortedFieldEntries(fields),
			UsedIn: sortedKeys(inputUsedIn[t]),
		}
	}
	for t, fields := range outputFields {
		out.Output[t] = jsonTypeEntry{
			Fields: sortedFieldEntries(fields),
			UsedIn: sortedKeys(outputUsedIn[t]),
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "error encoding json:", err)
		return 1
	}
	return 0
}

func printSection(side string, fields map[string]map[string]string, usedIn map[string]map[string]bool) {
	for _, name := range sortedKeys(fields) {
		ops := sortedKeys(usedIn[name])
		fmt.Printf("%s %s (used in: %s)\n", side, name, strings.Join(ops, ", "))
		for _, f := range sortedFieldEntries(fields[name]) {
			fmt.Printf("  %s: %s\n", f.Name, f.Type)
		}
	}
}

func collectInputType(schema *ast.Schema, typeName string, opName string, fields map[string]map[string]string, usedIn map[string]map[string]bool, visited map[string]bool) {
	def := schema.Types[typeName]
	if def == nil || def.Kind != ast.InputObject {
		return
	}
	if usedIn[typeName] == nil {
		usedIn[typeName] = map[string]bool{}
	}
	usedIn[typeName][opName] = true
	if _, exists := fields[typeName]; !exists {
		flds := map[string]string{}
		for _, f := range def.Fields {
			flds[f.Name] = f.Type.String()
		}
		fields[typeName] = flds
	}
	if visited[typeName] {
		return
	}
	visited[typeName] = true
	for _, f := range def.Fields {
		collectInputType(schema, f.Type.Name(), opName, fields, usedIn, visited)
	}
}

func collectOutputTypes(schema *ast.Schema, doc *ast.QueryDocument, sel ast.SelectionSet, parent *ast.Definition, opName string, skip map[string]bool, fields map[string]map[string]string, usedIn map[string]map[string]bool, visited map[string]bool) {
	for _, s := range sel {
		switch f := s.(type) {
		case *ast.Field:
			if f.Name == "__typename" {
				continue
			}
			fieldDef := parent.Fields.ForName(f.Name)
			if fieldDef == nil {
				continue
			}
			typeName := fieldDef.Type.Name()
			def := schema.Types[typeName]
			if def == nil || skip[typeName] {
				continue
			}
			if def.Kind == ast.Object || def.Kind == ast.Interface {
				if fields[typeName] == nil {
					fields[typeName] = map[string]string{}
				}
				if usedIn[typeName] == nil {
					usedIn[typeName] = map[string]bool{}
				}
				usedIn[typeName][opName] = true
				for _, sub := range f.SelectionSet {
					if sf, ok := sub.(*ast.Field); ok {
						if sf.Definition != nil {
							fields[typeName][sf.Name] = sf.Definition.Type.String()
						} else {
							fields[typeName][sf.Name] = ""
						}
					}
				}
				if !visited[typeName] {
					visited[typeName] = true
					collectOutputTypes(schema, doc, f.SelectionSet, def, opName, skip, fields, usedIn, visited)
				}
			}
		case *ast.InlineFragment:
			fragType := schema.Types[f.TypeCondition]
			if fragType == nil {
				fragType = parent
			}
			collectOutputTypes(schema, doc, f.SelectionSet, fragType, opName, skip, fields, usedIn, visited)
		case *ast.FragmentSpread:
			for _, frag := range doc.Fragments {
				if frag.Name == f.Name {
					fragType := schema.Types[frag.TypeCondition]
					if fragType == nil {
						fragType = parent
					}
					collectOutputTypes(schema, doc, frag.SelectionSet, fragType, opName, skip, fields, usedIn, visited)
				}
			}
		}
	}
}

func sortedFieldEntries(m map[string]string) []jsonFieldEntry {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	entries := make([]jsonFieldEntry, len(names))
	for i, n := range names {
		entries[i] = jsonFieldEntry{Name: n, Type: m[n]}
	}
	return entries
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
