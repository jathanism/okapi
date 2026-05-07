package typed

import (
	"fmt"
	"sort"
	"strings"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// operation is the IR for a single typed method.
type operation struct {
	OperationID string // raw operationId from spec
	GoName      string // PascalCase method name
	Method      string // upper-case HTTP verb
	Path        string // template, e.g. /api/v1/{aid}/contacts

	// Doc (from summary/description).
	Doc string

	// Params struct fields. Each field is one path/query/header param,
	// or a body field in the case of HasBody.
	Params []*paramField

	// Body details (only set when the operation declares a request body).
	HasBody    bool
	BodyType   string // Go type expression for the body field
	BodyJSON   bool   // true if application/json (the only kind we generate for)
	BodyDoc    string
	BodyField  string // "Body" — placed last in params struct
	BodyPtr    bool   // emit body as pointer if optional
	BodyOmit   bool   // omit empty?
	BodyReq    bool   // required
	BodyName   string // wire name (always "" for raw body, but kept for symmetry)

	// Response details.
	HasResult  bool
	ResultType string // Go type expression
	ResultPtr  bool   // emit as pointer in return signature
}

type paramField struct {
	GoName   string // PascalCase
	WireName string // path/query/header name on the wire
	GoType   string // Go type expression
	In       string // "path", "query", or "header"
	Required bool
	Doc      string
}

func (g *generator) collectOperations() error {
	if g.model.Paths == nil || g.model.Paths.PathItems == nil {
		return nil
	}

	for pair := g.model.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		path := pair.Key()
		item := pair.Value()
		ops := item.GetOperations()
		if ops == nil {
			continue
		}
		// Path-item-level parameters apply to every operation on that path.
		// Per OpenAPI 3, operation-level parameters with the same (name,in)
		// override the path-level entry.
		pathLevel := item.Parameters
		for opPair := ops.First(); opPair != nil; opPair = opPair.Next() {
			method := strings.ToUpper(opPair.Key())
			op := opPair.Value()
			built, err := g.translateOperation(method, path, op, pathLevel)
			if err != nil {
				return fmt.Errorf("%s %s: %w", method, path, err)
			}
			if built != nil {
				g.operations = append(g.operations, built)
			}
		}
	}

	sort.Slice(g.operations, func(i, j int) bool {
		return g.operations[i].GoName < g.operations[j].GoName
	})
	return nil
}

func (g *generator) translateOperation(method, path string, op *v3.Operation, pathLevel []*v3.Parameter) (*operation, error) {
	if op.OperationId == "" {
		// We require an operationId to generate a stable Go method name.
		return nil, nil
	}

	out := &operation{
		OperationID: op.OperationId,
		GoName:      pascal(op.OperationId),
		Method:      method,
		Path:        path,
		Doc:         buildOpDoc(op.Summary, op.Description),
	}

	// Parameters: merge path-level and operation-level. Operation-level
	// wins on conflicts (same name+in), per OpenAPI 3.
	merged := mergeParameters(pathLevel, op.Parameters)
	for _, p := range merged {
		if p == nil {
			continue
		}
		schema, err := g.translateSchema(p.Schema, out.GoName+pascal(p.Name))
		if err != nil {
			return nil, fmt.Errorf("param %s: %w", p.Name, err)
		}
		req := false
		if p.Required != nil {
			req = *p.Required
		}
		out.Params = append(out.Params, &paramField{
			GoName:   pascal(p.Name),
			WireName: p.Name,
			GoType:   schema.goExpr,
			In:       p.In,
			Required: req,
			Doc:      cleanDoc(p.Description),
		})
	}

	// Request body (JSON only).
	if op.RequestBody != nil && op.RequestBody.Content != nil {
		mt := op.RequestBody.Content.GetOrZero("application/json")
		if mt != nil && mt.Schema != nil {
			bodySchema, err := g.translateSchema(mt.Schema, out.GoName+"Body")
			if err != nil {
				return nil, fmt.Errorf("body: %w", err)
			}
			required := false
			if op.RequestBody.Required != nil {
				required = *op.RequestBody.Required
			}
			out.HasBody = true
			out.BodyJSON = true
			out.BodyType = bodySchema.goExpr
			out.BodyField = "Body"
			out.BodyReq = required
			out.BodyOmit = !required
			out.BodyPtr = !required && !isPointerlessKind(bodySchema)
			out.BodyDoc = cleanDoc(op.RequestBody.Description)
		}
	}

	// Response: prefer 2xx with application/json.
	if op.Responses != nil && op.Responses.Codes != nil {
		var bestCode string
		var bestSchema string
		for rPair := op.Responses.Codes.First(); rPair != nil; rPair = rPair.Next() {
			code := rPair.Key()
			if !isSuccess(code) {
				continue
			}
			r := rPair.Value()
			if r == nil || r.Content == nil {
				continue
			}
			mt := r.Content.GetOrZero("application/json")
			if mt == nil || mt.Schema == nil {
				continue
			}
			schema, err := g.translateSchema(mt.Schema, pascal(op.OperationId)+"Response")
			if err != nil {
				return nil, fmt.Errorf("response %s: %w", code, err)
			}
			// Empty `{}` schema becomes any — skip in favor of an actual
			// shaped response if we find one later.
			if schema.goExpr == "any" && bestSchema != "" {
				continue
			}
			if bestCode == "" || code < bestCode {
				bestCode = code
				bestSchema = schema.goExpr
			}
		}
		if bestSchema != "" && bestSchema != "any" {
			out.HasResult = true
			out.ResultType = bestSchema
			out.ResultPtr = true // we always return *T for shaped JSON responses
		}
	}

	return out, nil
}

func isPointerlessKind(t *goType) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case kindArray, kindMap, kindAny:
		return true
	}
	return false
}

func isSuccess(code string) bool {
	return strings.HasPrefix(code, "2")
}

// mergeParameters combines path-item-level and operation-level params,
// with operation-level winning on (name, in) collisions per OpenAPI 3.
func mergeParameters(pathLevel, opLevel []*v3.Parameter) []*v3.Parameter {
	if len(pathLevel) == 0 {
		return opLevel
	}
	type key struct{ name, in string }
	byKey := map[key]*v3.Parameter{}
	order := []key{}
	for _, p := range pathLevel {
		if p == nil {
			continue
		}
		k := key{p.Name, p.In}
		byKey[k] = p
		order = append(order, k)
	}
	for _, p := range opLevel {
		if p == nil {
			continue
		}
		k := key{p.Name, p.In}
		if _, exists := byKey[k]; !exists {
			order = append(order, k)
		}
		byKey[k] = p
	}
	out := make([]*v3.Parameter, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out
}

func buildOpDoc(summary, description string) string {
	summary = strings.TrimSpace(summary)
	description = strings.TrimSpace(description)
	switch {
	case summary != "" && description != "":
		return summary + "\n\n" + description
	case summary != "":
		return summary
	default:
		return description
	}
}
