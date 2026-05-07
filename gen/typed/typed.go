// Package typed generates a statically typed Go client (types + methods)
// from an OpenAPI 3.x spec. The output is self-contained: it depends only
// on the standard library, so consumers can vendor it without taking on
// okapi as a runtime dep.
//
// This is the static counterpart to okapi's dynamic dispatch (see the
// top-level openapi package). Use this when you want compile-time
// guarantees on parameter shapes, request bodies, and response types,
// and a SemVer-tracked surface that only changes via deliberate regen.
package typed

import (
	"fmt"
	"go/format"
	"sort"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Options control generator output. The zero value is invalid — at minimum,
// PackageName must be set, and exactly one of Source or SpecBytes.
type Options struct {
	// PackageName is the Go package name for the generated files
	// (e.g. "contacts"). Must be a valid Go identifier.
	PackageName string

	// Source is a file:// or http(s):// URL of the OpenAPI spec, or a raw
	// path. Mutually exclusive with SpecBytes.
	Source string

	// SpecBytes is the OpenAPI spec as raw bytes (YAML or JSON). Mutually
	// exclusive with Source.
	SpecBytes []byte

	// ClientName is the name of the generated client struct.
	// Defaults to "Client".
	ClientName string

	// ModulePath, if set, is written into the generated file headers as a
	// hint about where this code lives. Optional.
	ModulePath string
}

// Files is the set of generated files keyed by basename. Values are
// gofmt-clean Go source.
type Files map[string][]byte

// Generate runs the generator and returns the file set. Callers write
// the files to disk; Generate itself touches no files.
func Generate(opts Options) (Files, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if opts.ClientName == "" {
		opts.ClientName = "Client"
	}

	model, err := loadSpec(opts)
	if err != nil {
		return nil, err
	}

	g := &generator{opts: opts, model: model}
	if err := g.collect(); err != nil {
		return nil, err
	}

	out := Files{}

	typesSrc, err := g.renderTypes()
	if err != nil {
		return nil, fmt.Errorf("render types: %w", err)
	}
	out["types.gen.go"], err = formatGo(typesSrc)
	if err != nil {
		return nil, fmt.Errorf("format types: %w\n--- raw ---\n%s", err, typesSrc)
	}

	clientSrc, err := g.renderClient()
	if err != nil {
		return nil, fmt.Errorf("render client: %w", err)
	}
	out["client.gen.go"], err = formatGo(clientSrc)
	if err != nil {
		return nil, fmt.Errorf("format client: %w\n--- raw ---\n%s", err, clientSrc)
	}

	return out, nil
}

func (o Options) validate() error {
	if o.PackageName == "" {
		return fmt.Errorf("PackageName is required")
	}
	if !isGoIdent(o.PackageName) {
		return fmt.Errorf("PackageName %q is not a valid Go identifier", o.PackageName)
	}
	if o.Source == "" && len(o.SpecBytes) == 0 {
		return fmt.Errorf("Source or SpecBytes is required")
	}
	if o.Source != "" && len(o.SpecBytes) != 0 {
		return fmt.Errorf("Source and SpecBytes are mutually exclusive")
	}
	return nil
}

func loadSpec(opts Options) (*v3.Document, error) {
	var data []byte
	var err error
	if len(opts.SpecBytes) > 0 {
		data = opts.SpecBytes
	} else {
		data, err = readSource(opts.Source)
		if err != nil {
			return nil, fmt.Errorf("read spec: %w", err)
		}
	}

	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}

	// libopenapi can panic when handed YAML that parses but isn't a
	// recognizable OpenAPI 3 document. Recover so the generator
	// surfaces a clean error instead of taking the caller down.
	model, buildErr := buildModel(doc)
	if buildErr != nil {
		return nil, fmt.Errorf("build v3 model: %w", buildErr)
	}
	if model == nil {
		return nil, fmt.Errorf("build v3 model: input does not appear to be an OpenAPI 3 document")
	}
	return &model.Model, nil
}

func buildModel(doc libopenapi.Document) (m *libopenapi.DocumentModel[v3.Document], err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("input does not appear to be an OpenAPI 3 document (libopenapi panicked: %v)", r)
		}
	}()
	var rawErr error
	m, rawErr = doc.BuildV3Model()
	if rawErr != nil {
		return nil, rawErr
	}
	return m, nil
}

func formatGo(src string) ([]byte, error) {
	return format.Source([]byte(src))
}

// generator is the per-run state shared across renderers.
type generator struct {
	opts  Options
	model *v3.Document

	// schema name (PascalCase) -> goType describing the struct/alias.
	schemas    map[string]*goType
	schemaKeys []string

	// operations in deterministic order.
	operations []*operation
}

func (g *generator) collect() error {
	g.schemas = map[string]*goType{}

	if g.model.Components != nil && g.model.Components.Schemas != nil {
		// libopenapi exposes Schemas as an ordered map; range gives stable order.
		for pair := g.model.Components.Schemas.First(); pair != nil; pair = pair.Next() {
			name := pair.Key()
			schema := pair.Value()
			t, err := g.translateSchema(schema, name)
			if err != nil {
				return fmt.Errorf("schema %s: %w", name, err)
			}
			g.registerNamedType(name, t)
		}
	}

	if err := g.collectOperations(); err != nil {
		return err
	}

	sort.Strings(g.schemaKeys)
	return nil
}

// registerNamedType records a top-level type declaration. Called for
// every named struct/enum we want emitted in types.gen.go — both
// components.schemas walks and inline-enum synthesis. Re-registering
// the same name is a no-op (last write wins on body), keeping output
// stable when the same hint surfaces from multiple paths.
func (g *generator) registerNamedType(name string, t *goType) {
	if name == "" || t == nil {
		return
	}
	if _, exists := g.schemas[name]; !exists {
		g.schemaKeys = append(g.schemaKeys, name)
	}
	g.schemas[name] = t
}
