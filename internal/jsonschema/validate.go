// Package jsonschema is a thin wrapper over santhosh-tekuri/jsonschema/v5
// that returns errors structured for our reporting layer.
package jsonschema

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"sigs.k8s.io/yaml"
)

// Validator wraps a compiled JSON Schema.
type Validator struct {
	schema *jsonschema.Schema
}

// New compiles the given JSON-Schema document.
func New(name string, schemaBytes []byte) (*Validator, error) {
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, bytes.NewReader(schemaBytes)); err != nil {
		return nil, fmt.Errorf("add schema %s: %w", name, err)
	}
	s, err := c.Compile(name)
	if err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", name, err)
	}
	return &Validator{schema: s}, nil
}

// ValidateYAML parses YAML, then validates it against the schema. The decoded
// value is returned (as map[string]any) so callers don't have to parse twice.
func (v *Validator) ValidateYAML(data []byte) (any, error) {
	jsonBytes, err := yaml.YAMLToJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	var inst any
	if err := yaml.Unmarshal(jsonBytes, &inst); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := v.schema.Validate(inst); err != nil {
		return inst, FormatError(err)
	}
	return inst, nil
}

// FormatError flattens a jsonschema validation error into a single human-
// readable string with one line per issue (path: message).
func FormatError(err error) error {
	if err == nil {
		return nil
	}
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return err
	}
	var b strings.Builder
	walk(&b, ve, 0)
	return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
}

func walk(b *strings.Builder, ve *jsonschema.ValidationError, depth int) {
	loc := ve.InstanceLocation
	if loc == "" {
		loc = "<root>"
	}
	if msg := strings.TrimSpace(ve.Message); msg != "" {
		fmt.Fprintf(b, "%s%s: %s\n", strings.Repeat("  ", depth), loc, msg)
	}
	for _, c := range ve.Causes {
		walk(b, c, depth+1)
	}
}
