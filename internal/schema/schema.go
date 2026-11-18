// Package schema compiles JSON Schemas and validates documents against them.
package schema

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var (
	cache   sync.Map // sha256 of schema -> *jsonschema.Schema
	printer = message.NewPrinter(language.English)
)

// Compile compiles a JSON Schema document (draft 2020-12 unless $schema says otherwise).
func Compile(b []byte) (*jsonschema.Schema, error) {
	key := sha256.Sum256(b)
	if s, ok := cache.Load(key); ok {
		return s.(*jsonschema.Schema), nil
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("mem://schema.json", doc); err != nil {
		return nil, err
	}
	s, err := c.Compile("mem://schema.json")
	if err != nil {
		return nil, err
	}
	cache.Store(key, s)
	return s, nil
}

// Check validates a JSON document and returns its problems; nil means valid.
func Check(schemaJSON, doc []byte) []string {
	s, err := Compile(schemaJSON)
	if err != nil {
		return []string{"schema: " + err.Error()}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return []string{"not valid JSON: " + err.Error()}
	}
	err = s.Validate(inst)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if errors.As(err, &ve) {
		return Leaves(ve)
	}
	return []string{err.Error()}
}

// Leaves flattens a validation error into "location: message" lines.
func Leaves(ve *jsonschema.ValidationError) []string {
	if len(ve.Causes) == 0 {
		return []string{"/" + strings.Join(ve.InstanceLocation, "/") + ": " + ve.ErrorKind.LocalizedString(printer)}
	}
	var out []string
	for _, c := range ve.Causes {
		out = append(out, Leaves(c)...)
	}
	return out
}
