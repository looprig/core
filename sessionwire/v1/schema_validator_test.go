package v1_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// This file implements the small JSON Schema evaluator the published V1
// contract actually needs. The package may not take a runtime schema
// dependency (see deps_test.go), and a member-name-presence check is not
// validation: it would accept a schema declaring "journal_seq" as a string, a
// contradictory minimum/const/minItems, or a dangling "$ref". Wave 2 TypeScript
// drift-checking consumes these files, so they must be kept honest here.
//
// The evaluator supports exactly the keyword set the 33 published schemas use
// and FAILS LOUDLY on anything else, so an unsupported keyword can never be
// silently ignored. Extending the schemas with a new keyword is therefore a
// deliberate act that also extends this evaluator.
var (
	// v1AssertionKeywords are evaluated.
	v1AssertionKeywords = map[string]struct{}{
		"$ref": {}, "type": {}, "required": {}, "properties": {},
		"additionalProperties": {}, "enum": {}, "const": {}, "items": {},
		"minItems": {}, "uniqueItems": {}, "minLength": {}, "minimum": {},
		"oneOf": {}, "anyOf": {}, "not": {},
	}
	// v1AnnotationKeywords carry no assertion in the 2020-12 dialect as used
	// here. "format" is annotation-only by default and is deliberately not
	// treated as an assertion.
	v1AnnotationKeywords = map[string]struct{}{
		"$schema": {}, "$id": {}, "title": {}, "description": {},
		"format": {}, "$defs": {},
	}
)

// TestV1SchemasUseOnlySupportedKeywords walks every subschema position in every
// published schema and rejects a keyword the evaluator does not implement. It is
// what makes "every fixture validates" a real statement rather than a partial one.
func TestV1SchemasUseOnlySupportedKeywords(t *testing.T) {
	t.Parallel()

	for _, path := range v1SchemaFiles(t) {
		path := path
		t.Run(shortName(path), func(t *testing.T) {
			t.Parallel()
			document := readV1SchemaDocument(t, path)
			for _, problem := range unsupportedV1Keywords(document, "#") {
				t.Error(problem)
			}
		})
	}
}

// TestV1FixturesValidateAgainstTheirSchemas is F1.5 step 2's real assertion.
func TestV1FixturesValidateAgainstTheirSchemas(t *testing.T) {
	t.Parallel()

	for _, fixturePath := range v1FixtureFiles(t) {
		fixturePath := fixturePath
		stem := strings.TrimSuffix(shortName(fixturePath), ".json")
		t.Run(stem, func(t *testing.T) {
			t.Parallel()

			document := readV1SchemaDocument(t, v1SchemaDir+"/"+stem+".schema.json")
			instance := readV1Instance(t, fixturePath)
			problems, err := validateV1Instance(document, instance)
			if err != nil {
				t.Fatalf("evaluate schema: %v", err)
			}
			for _, problem := range problems {
				t.Errorf("fixture does not validate: %s", problem)
			}
		})
	}
}

// TestV1SchemaValidatorFiresOnDrift proves the evaluator is load-bearing by
// mutating a known-good schema/fixture pair in memory in the exact ways the
// previous member-name-presence check could not detect.
func TestV1SchemaValidatorFiresOnDrift(t *testing.T) {
	t.Parallel()

	const stem = "enduring_publication"
	base := readV1SchemaDocument(t, v1SchemaDir+"/"+stem+".schema.json")
	fixture := readV1Instance(t, v1FixtureDir+"/"+stem+".json")

	if problems, err := validateV1Instance(base, fixture); err != nil || len(problems) != 0 {
		t.Fatalf("baseline pair must validate: %v %v", problems, err)
	}

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "wrong declared type",
			mutate: func(schema map[string]any) {
				properties(schema)["journal_seq"] = map[string]any{"type": "string"}
			},
		},
		{
			name: "contradictory numeric bound",
			mutate: func(schema map[string]any) {
				properties(schema)["journal_seq"] = map[string]any{"type": "integer", "minimum": json.Number("99")}
			},
		},
		{
			name: "contradictory const",
			mutate: func(schema map[string]any) {
				properties(schema)["journal_seq"] = map[string]any{"const": json.Number("99")}
			},
		},
		{
			name: "contradictory minItems",
			mutate: func(schema map[string]any) {
				properties(schema)["event_id"] = map[string]any{"type": "array", "minItems": json.Number("3")}
			},
		},
		{
			name: "dangling local reference",
			mutate: func(schema map[string]any) {
				properties(schema)["tenant_id"] = map[string]any{"$ref": "#/$defs/typo"}
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mutated := cloneJSONValue(base).(map[string]any)
			tt.mutate(mutated)
			problems, err := validateV1Instance(mutated, fixture)
			if err == nil && len(problems) == 0 {
				t.Fatal("mutated schema still accepted the fixture; the evaluator is not load-bearing")
			}
		})
	}

	t.Run("unsupported keyword fails loudly", func(t *testing.T) {
		t.Parallel()

		mutated := cloneJSONValue(base).(map[string]any)
		properties(mutated)["journal_seq"] = map[string]any{"type": "integer", "exclusiveMaximum": json.Number("3")}
		if problems := unsupportedV1Keywords(mutated, "#"); len(problems) == 0 {
			t.Fatal("unsupported keyword was silently accepted")
		}
		if _, err := validateV1Instance(mutated, fixture); err == nil {
			t.Fatal("evaluator silently ignored an unimplemented keyword")
		}
	})

	t.Run("drifted fixture", func(t *testing.T) {
		t.Parallel()

		drifted := cloneJSONValue(fixture).(map[string]any)
		drifted["journal_seq"] = "five"
		problems, err := validateV1Instance(base, drifted)
		if err != nil {
			t.Fatalf("evaluate schema: %v", err)
		}
		if len(problems) == 0 {
			t.Fatal("a fixture whose member has the wrong JSON type was accepted")
		}
	})
}

func properties(schema map[string]any) map[string]any {
	return schema["properties"].(map[string]any)
}

// unsupportedV1Keywords reports every keyword in every subschema position that
// the evaluator does not implement.
func unsupportedV1Keywords(schema any, path string) []string {
	switch typed := schema.(type) {
	case bool:
		return nil
	case map[string]any:
		var problems []string
		names := make([]string, 0, len(typed))
		for name := range typed {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value := typed[name]
			if _, ok := v1AssertionKeywords[name]; !ok {
				if _, annotation := v1AnnotationKeywords[name]; !annotation {
					problems = append(problems, fmt.Sprintf("%s: keyword %q is not implemented by the V1 schema evaluator", path, name))
					continue
				}
			}
			switch name {
			case "properties", "$defs":
				members, ok := value.(map[string]any)
				if !ok {
					problems = append(problems, fmt.Sprintf("%s/%s: expected an object of subschemas", path, name))
					continue
				}
				for member, subschema := range members {
					problems = append(problems, unsupportedV1Keywords(subschema, path+"/"+name+"/"+member)...)
				}
			case "items", "not", "additionalProperties":
				problems = append(problems, unsupportedV1Keywords(value, path+"/"+name)...)
			case "oneOf", "anyOf":
				branches, ok := value.([]any)
				if !ok {
					problems = append(problems, fmt.Sprintf("%s/%s: expected an array of subschemas", path, name))
					continue
				}
				for index, branch := range branches {
					problems = append(problems, unsupportedV1Keywords(branch, fmt.Sprintf("%s/%s/%d", path, name, index))...)
				}
			}
		}
		return problems
	default:
		return []string{fmt.Sprintf("%s: subschema is neither an object nor a boolean", path)}
	}
}

// validateV1Instance evaluates instance against the root schema document. A
// returned error means the schema itself is unusable (an unimplemented keyword
// or an unresolvable reference); returned problems mean the instance is invalid.
func validateV1Instance(root map[string]any, instance any) ([]string, error) {
	evaluator := &v1Evaluator{root: root}
	problems := evaluator.evaluate(root, instance, "$")
	if evaluator.fatal != nil {
		return nil, evaluator.fatal
	}
	return problems, nil
}

type v1Evaluator struct {
	root  map[string]any
	fatal error
}

func (e *v1Evaluator) fail(format string, args ...any) {
	if e.fatal == nil {
		e.fatal = fmt.Errorf(format, args...)
	}
}

func (e *v1Evaluator) evaluate(schema any, instance any, path string) []string {
	switch typed := schema.(type) {
	case bool:
		if typed {
			return nil
		}
		return []string{path + ": schema forbids any value here"}
	case map[string]any:
		return e.evaluateObject(typed, instance, path)
	default:
		e.fail("%s: subschema is neither an object nor a boolean", path)
		return nil
	}
}

func (e *v1Evaluator) evaluateObject(schema map[string]any, instance any, path string) []string {
	var problems []string
	names := make([]string, 0, len(schema))
	for name := range schema {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		value := schema[name]
		if _, annotation := v1AnnotationKeywords[name]; annotation {
			continue
		}
		if _, supported := v1AssertionKeywords[name]; !supported {
			e.fail("%s: keyword %q is not implemented by the V1 schema evaluator", path, name)
			return problems
		}
		switch name {
		case "$ref":
			target, err := e.resolve(value)
			if err != nil {
				e.fail("%s/$ref: %v", path, err)
				return problems
			}
			problems = append(problems, e.evaluate(target, instance, path)...)
		case "type":
			if !e.matchesType(value, instance, path) {
				problems = append(problems, fmt.Sprintf("%s: value %s does not have type %v", path, describeJSON(instance), value))
			}
		case "const":
			if !jsonValueEqual(value, instance) {
				problems = append(problems, fmt.Sprintf("%s: value %s is not the required const %s", path, describeJSON(instance), describeJSON(value)))
			}
		case "enum":
			options, ok := value.([]any)
			if !ok {
				e.fail("%s/enum: expected an array", path)
				return problems
			}
			matched := false
			for _, option := range options {
				if jsonValueEqual(option, instance) {
					matched = true
					break
				}
			}
			if !matched {
				problems = append(problems, fmt.Sprintf("%s: value %s is not one of the enumerated values", path, describeJSON(instance)))
			}
		case "required":
			object, ok := instance.(map[string]any)
			if !ok {
				continue
			}
			members, ok := value.([]any)
			if !ok {
				e.fail("%s/required: expected an array", path)
				return problems
			}
			for _, member := range members {
				name, ok := member.(string)
				if !ok {
					e.fail("%s/required: expected member names", path)
					return problems
				}
				if _, present := object[name]; !present {
					problems = append(problems, fmt.Sprintf("%s: missing required member %q", path, name))
				}
			}
		case "properties":
			object, ok := instance.(map[string]any)
			if !ok {
				continue
			}
			declared, ok := value.(map[string]any)
			if !ok {
				e.fail("%s/properties: expected an object", path)
				return problems
			}
			memberNames := make([]string, 0, len(object))
			for member := range object {
				memberNames = append(memberNames, member)
			}
			sort.Strings(memberNames)
			for _, member := range memberNames {
				subschema, declaredMember := declared[member]
				if !declaredMember {
					continue
				}
				problems = append(problems, e.evaluate(subschema, object[member], path+"."+member)...)
			}
		case "additionalProperties":
			object, ok := instance.(map[string]any)
			if !ok {
				continue
			}
			declared, _ := schema["properties"].(map[string]any)
			memberNames := make([]string, 0, len(object))
			for member := range object {
				memberNames = append(memberNames, member)
			}
			sort.Strings(memberNames)
			for _, member := range memberNames {
				if _, declaredMember := declared[member]; declaredMember {
					continue
				}
				problems = append(problems, e.evaluate(value, object[member], path+"."+member)...)
			}
		case "items":
			array, ok := instance.([]any)
			if !ok {
				continue
			}
			for index, element := range array {
				problems = append(problems, e.evaluate(value, element, fmt.Sprintf("%s[%d]", path, index))...)
			}
		case "minItems":
			array, ok := instance.([]any)
			if !ok {
				continue
			}
			bound, ok := e.number(value, path+"/minItems")
			if !ok {
				return problems
			}
			if big.NewFloat(float64(len(array))).Cmp(bound) < 0 {
				problems = append(problems, fmt.Sprintf("%s: array has %d items, fewer than the required minimum %s", path, len(array), bound.Text('f', -1)))
			}
		case "uniqueItems":
			array, ok := instance.([]any)
			if !ok {
				continue
			}
			unique, ok := value.(bool)
			if !ok {
				e.fail("%s/uniqueItems: expected a boolean", path)
				return problems
			}
			if unique {
				for i := range array {
					for j := i + 1; j < len(array); j++ {
						if jsonValueEqual(array[i], array[j]) {
							problems = append(problems, fmt.Sprintf("%s: items %d and %d are duplicates", path, i, j))
						}
					}
				}
			}
		case "minLength":
			text, ok := instance.(string)
			if !ok {
				continue
			}
			bound, ok := e.number(value, path+"/minLength")
			if !ok {
				return problems
			}
			if big.NewFloat(float64(utf8.RuneCountInString(text))).Cmp(bound) < 0 {
				problems = append(problems, fmt.Sprintf("%s: string is shorter than the required minimum length %s", path, bound.Text('f', -1)))
			}
		case "minimum":
			actual, ok := jsonNumber(instance)
			if !ok {
				continue
			}
			bound, ok := e.number(value, path+"/minimum")
			if !ok {
				return problems
			}
			if actual.Cmp(bound) < 0 {
				problems = append(problems, fmt.Sprintf("%s: value %s is below the required minimum %s", path, actual.Text('f', -1), bound.Text('f', -1)))
			}
		case "not":
			if inner := e.evaluate(value, instance, path); e.fatal == nil && len(inner) == 0 {
				problems = append(problems, fmt.Sprintf("%s: value %s matches a forbidden subschema", path, describeJSON(instance)))
			}
		case "oneOf", "anyOf":
			branches, ok := value.([]any)
			if !ok {
				e.fail("%s/%s: expected an array of subschemas", path, name)
				return problems
			}
			matches := 0
			for _, branch := range branches {
				if inner := e.evaluate(branch, instance, path); e.fatal == nil && len(inner) == 0 {
					matches++
				}
			}
			if e.fatal != nil {
				return problems
			}
			switch {
			case name == "anyOf" && matches == 0:
				problems = append(problems, fmt.Sprintf("%s: value %s matches no anyOf branch", path, describeJSON(instance)))
			case name == "oneOf" && matches != 1:
				problems = append(problems, fmt.Sprintf("%s: value %s matches %d oneOf branches, want exactly 1", path, describeJSON(instance), matches))
			}
		}
	}
	return problems
}

// resolve dereferences a local JSON pointer. A remote or unresolvable reference
// is a schema defect, not an instance defect, so it is reported as fatal.
func (e *v1Evaluator) resolve(value any) (any, error) {
	pointer, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("expected a string reference")
	}
	if !strings.HasPrefix(pointer, "#") {
		return nil, fmt.Errorf("only local references are supported, got %q", pointer)
	}
	var current any = e.root
	for _, token := range strings.Split(strings.TrimPrefix(pointer, "#"), "/") {
		if token == "" {
			continue
		}
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("reference %q traverses a non-object", pointer)
		}
		next, ok := object[token]
		if !ok {
			return nil, fmt.Errorf("reference %q does not resolve", pointer)
		}
		current = next
	}
	return current, nil
}

func (e *v1Evaluator) number(value any, where string) (*big.Float, bool) {
	number, ok := jsonNumber(value)
	if !ok {
		e.fail("%s: expected a number", where)
		return nil, false
	}
	return number, true
}

func (e *v1Evaluator) matchesType(value any, instance any, path string) bool {
	switch typed := value.(type) {
	case string:
		return matchesJSONType(typed, instance)
	case []any:
		for _, option := range typed {
			name, ok := option.(string)
			if !ok {
				e.fail("%s/type: expected type names", path)
				return true
			}
			if matchesJSONType(name, instance) {
				return true
			}
		}
		return false
	default:
		e.fail("%s/type: expected a string or array of strings", path)
		return true
	}
}

func matchesJSONType(name string, instance any) bool {
	switch name {
	case "object":
		_, ok := instance.(map[string]any)
		return ok
	case "array":
		_, ok := instance.([]any)
		return ok
	case "string":
		_, ok := instance.(string)
		return ok
	case "boolean":
		_, ok := instance.(bool)
		return ok
	case "null":
		return instance == nil
	case "number":
		_, ok := jsonNumber(instance)
		return ok
	case "integer":
		number, ok := instance.(json.Number)
		if !ok {
			return false
		}
		if _, err := number.Int64(); err == nil {
			return true
		}
		value, ok := new(big.Float).SetString(number.String())
		return ok && value.IsInt()
	default:
		return false
	}
}

func jsonNumber(instance any) (*big.Float, bool) {
	number, ok := instance.(json.Number)
	if !ok {
		return nil, false
	}
	value, ok := new(big.Float).SetString(number.String())
	return value, ok
}

func jsonValueEqual(left, right any) bool {
	leftNumber, leftIsNumber := jsonNumber(left)
	rightNumber, rightIsNumber := jsonNumber(right)
	if leftIsNumber || rightIsNumber {
		return leftIsNumber && rightIsNumber && leftNumber.Cmp(rightNumber) == 0
	}
	return reflect.DeepEqual(left, right)
}

func describeJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for name, member := range typed {
			clone[name] = cloneJSONValue(member)
		}
		return clone
	case []any:
		clone := make([]any, len(typed))
		for index, element := range typed {
			clone[index] = cloneJSONValue(element)
		}
		return clone
	default:
		return value
	}
}

func readV1SchemaDocument(t *testing.T, path string) map[string]any {
	t.Helper()
	value := readV1Instance(t, path)
	document, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("schema %s is not a JSON object", path)
	}
	return document
}

func readV1Instance(t *testing.T, path string) any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return value
}

func shortName(path string) string {
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}
