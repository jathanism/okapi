package typed

import (
	"fmt"
	"strings"

	base "github.com/pb33f/libopenapi/datamodel/high/base"
	yaml "go.yaml.in/yaml/v4"
)

// goType is the in-memory IR for a Go type declaration. We only need
// enough to render either a named type alias or a struct, plus enough
// inline-type metadata to render fields.
type goType struct {
	// Name is the PascalCase Go identifier (only set for named types
	// declared at top level — components.schemas).
	Name string

	// Doc is an optional comment.
	Doc string

	// Kind tells the renderer what to emit.
	Kind goKind

	// For Kind == kindStruct.
	Fields []*goField

	// For Kind == kindEnum, kindAlias, kindArray, kindMap, kindPrimitive.
	// goExpr is the rendered Go type expression for use in field/param
	// positions (e.g. "string", "[]Contact", "map[string]any").
	goExpr string

	// EnumValues is set for Kind == kindEnum (string enums only for now).
	EnumValues []string
}

type goKind int

const (
	kindPrimitive goKind = iota
	kindAlias            // named alias of a primitive (e.g. type Status string)
	kindEnum             // string enum: type Status string + const block
	kindStruct
	kindArray
	kindMap
	kindAny
)

type goField struct {
	GoName    string // PascalCase
	JSONName  string // wire name
	GoType    string // rendered type expression
	OmitEmpty bool   // emitted if not required
	Doc       string
	Pointer   bool // true => emit as pointer (nullable / optional non-primitive)
}

// translateSchema converts a libopenapi schema proxy into a goType. The
// hint is a candidate name, used when we have to synthesize one for an
// inline schema. For top-level component schemas, hint is the schema's
// declared name.
func (g *generator) translateSchema(proxy *base.SchemaProxy, hint string) (*goType, error) {
	if proxy == nil {
		return &goType{Kind: kindAny, goExpr: "any"}, nil
	}

	// $ref to a component: emit a name reference. We rely on the referenced
	// schema being present in components.schemas, which collect() walks.
	if ref := proxy.GetReference(); ref != "" {
		name := refName(ref)
		if name == "" {
			return nil, fmt.Errorf("unsupported $ref: %s", ref)
		}
		return &goType{Kind: kindAlias, goExpr: name}, nil
	}

	schema := proxy.Schema()
	if schema == nil {
		return &goType{Kind: kindAny, goExpr: "any"}, nil
	}

	// allOf with a single member is just that member. We don't attempt to
	// merge multi-member allOf — caller gets `any` and a TODO. oneOf/anyOf
	// likewise fall through to `any` for the MVP.
	if len(schema.AllOf) == 1 {
		return g.translateSchema(schema.AllOf[0], hint)
	}
	if len(schema.AllOf) > 1 || len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 {
		return &goType{Kind: kindAny, goExpr: "any"}, nil
	}

	primary, nullable := primaryType(schema.Type)

	switch primary {
	case "string":
		if len(schema.Enum) > 0 {
			values := stringEnumFromNodes(schema.Enum)
			if len(values) > 0 && hint != "" {
				name := pascal(hint)
				t := &goType{
					Kind:       kindEnum,
					Name:       name,
					goExpr:     name,
					EnumValues: values,
				}
				g.registerNamedType(name, t)
				return t, nil
			}
		}
		return &goType{Kind: kindPrimitive, goExpr: "string"}, nil

	case "integer":
		expr := "int64"
		if schema.Format == "int32" {
			expr = "int32"
		}
		return &goType{Kind: kindPrimitive, goExpr: expr}, nil

	case "number":
		expr := "float64"
		if schema.Format == "float" {
			expr = "float32"
		}
		return &goType{Kind: kindPrimitive, goExpr: expr}, nil

	case "boolean":
		return &goType{Kind: kindPrimitive, goExpr: "bool"}, nil

	case "array":
		var elem *goType
		var err error
		if schema.Items != nil && schema.Items.A != nil {
			elem, err = g.translateSchema(schema.Items.A, hint+"Item")
			if err != nil {
				return nil, fmt.Errorf("array items: %w", err)
			}
		} else {
			elem = &goType{Kind: kindAny, goExpr: "any"}
		}
		return &goType{Kind: kindArray, goExpr: "[]" + elem.goExpr}, nil

	case "object":
		// additionalProperties only (no properties) → map[string]<elem>.
		if schema.Properties == nil || schema.Properties.Len() == 0 {
			if schema.AdditionalProperties != nil && schema.AdditionalProperties.A != nil {
				elem, err := g.translateSchema(schema.AdditionalProperties.A, hint+"Value")
				if err != nil {
					return nil, fmt.Errorf("additionalProperties: %w", err)
				}
				return &goType{Kind: kindMap, goExpr: "map[string]" + elem.goExpr}, nil
			}
			// Empty object → map[string]any.
			return &goType{Kind: kindMap, goExpr: "map[string]any"}, nil
		}

		// Named struct.
		t := &goType{
			Kind:   kindStruct,
			Name:   pascal(hint),
			goExpr: pascal(hint),
			Doc:    cleanDoc(schema.Description),
		}
		required := stringSet(schema.Required)
		for pair := schema.Properties.First(); pair != nil; pair = pair.Next() {
			propName := pair.Key()
			propSchema := pair.Value()
			fieldType, err := g.translateSchema(propSchema, hint+pascal(propName))
			if err != nil {
				return nil, fmt.Errorf("property %s: %w", propName, err)
			}
			isRequired := required[propName]
			isNullable := propIsNullable(propSchema)
			f := &goField{
				GoName:    pascal(propName),
				JSONName:  propName,
				GoType:    fieldType.goExpr,
				OmitEmpty: !isRequired,
				Pointer:   shouldPointer(fieldType, isRequired, isNullable),
			}
			if propSchema.Schema() != nil {
				f.Doc = cleanDoc(propSchema.Schema().Description)
			}
			// Optional array/map fields carry a pointer purely for wire
			// presence; spell out the contract where no description
			// already does.
			if f.Doc == "" && f.Pointer && f.OmitEmpty {
				switch fieldType.Kind {
				case kindArray:
					f.Doc = fmt.Sprintf("nil omits the property; &%s{} sends [] (clears it).", fieldType.goExpr)
				case kindMap:
					f.Doc = fmt.Sprintf("nil omits the property; &%s{} sends {} (clears it).", fieldType.goExpr)
				}
			}
			t.Fields = append(t.Fields, f)
		}
		return t, nil

	case "":
		// No type given → fall back to any.
		return &goType{Kind: kindAny, goExpr: "any"}, nil
	}

	_ = nullable
	return nil, fmt.Errorf("unsupported schema type %q (hint=%s)", primary, hint)
}

// primaryType collapses OpenAPI 3.1's `type: [string, "null"]` form into
// a single primary type plus a nullable flag.
func primaryType(types []string) (primary string, nullable bool) {
	for _, t := range types {
		if t == "null" {
			nullable = true
			continue
		}
		if primary == "" {
			primary = t
		}
	}
	return
}

func propIsNullable(p *base.SchemaProxy) bool {
	if p == nil {
		return false
	}
	s := p.Schema()
	if s == nil {
		return false
	}
	for _, t := range s.Type {
		if t == "null" {
			return true
		}
	}
	return false
}

// shouldPointer decides whether a struct field gets a pointer. Rules:
//   - Required (non-nullable) anything → bare value.
//   - Optional or nullable scalar/struct → pointer (so absence vs zero
//     is distinguishable on the wire).
//   - Optional array/map (incl. free-form objects, map[string]any) →
//     pointer. Optional fields carry omitempty, which drops any bare
//     slice/map with len == 0, nil or not — making tri-state PATCH
//     semantics (omit vs clear vs replace) unreachable. Required
//     nullable *inline* arrays/maps have no omitempty, and a bare nil
//     slice/map already marshals as null, so they stay bare: the
//     pointer would unlock no new wire state. ($ref'd array/map
//     components arrive as kindAlias and still take the scalar rule —
//     pointer when nullable.)
//   - `any` stays non-pointer; it can already hold nil.
func shouldPointer(t *goType, required, nullable bool) bool {
	if t == nil || t.Kind == kindAny {
		return false
	}
	if t.Kind == kindArray || t.Kind == kindMap {
		return !required
	}
	return !required || nullable
}

func stringEnumFromNodes(values []*yaml.Node) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == nil || v.Kind != yaml.ScalarNode {
			return nil
		}
		out = append(out, v.Value)
	}
	return out
}

func stringSet(in []string) map[string]bool {
	m := make(map[string]bool, len(in))
	for _, s := range in {
		m[s] = true
	}
	return m
}

func refName(ref string) string {
	const prefix = "#/components/schemas/"
	if strings.HasPrefix(ref, prefix) {
		return pascal(strings.TrimPrefix(ref, prefix))
	}
	return ""
}

func cleanDoc(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return s
}
