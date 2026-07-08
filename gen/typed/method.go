package typed

import (
	"fmt"
	"mime"
	"sort"
	"strings"

	base "github.com/pb33f/libopenapi/datamodel/high/base"
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
	BodyJSON   bool   // true if application/json
	BodyStream bool   // true if binary media streamed as io.Reader
	BodyMedia  string // declared media type for streamed bodies
	BodyDoc    string
	BodyField  string // "Body" — placed last in params struct
	BodyPtr    bool   // emit body as pointer if optional
	BodyOmit   bool   // omit empty?
	BodyReq    bool   // required
	BodyName   string // wire name (always "" for raw body, but kept for symmetry)

	// Response details.
	HasResult    bool
	ResultType   string // Go type expression
	ResultPtr    bool   // emit as pointer in return signature
	ResultStream bool   // true if the 2xx body is non-JSON — returned as io.ReadCloser
	ResultMedia  string // declared media type of the streamed response

	// Response headers declared on the primary success response. When
	// non-empty, the method returns an extra HeadersName struct value.
	RespHeaders []*respHeaderField
	HeadersName string // "<GoName>ResponseHeaders"
}

type paramField struct {
	GoName   string // PascalCase
	WireName string // path/query/header name on the wire
	GoType   string // Go type expression
	In       string // "path", "query", or "header"
	Required bool
	Doc      string
}

// respHeaderField is one declared response header, surfaced as a field
// on the generated <Op>ResponseHeaders struct.
type respHeaderField struct {
	GoName   string // PascalCase
	WireName string // header name on the wire
	GoType   string // Go type expression for the struct field
	Parse    string // decode strategy: "string", "cast", "int32", "int64", "float32", "float64", or "bool"
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

	// Request body: application/json is encoded from a typed struct;
	// binary media (application/octet-stream, or any non-JSON media type
	// whose schema is `type: string, format: binary`) is streamed from an
	// io.Reader. Anything else is ignored.
	if op.RequestBody != nil && op.RequestBody.Content != nil {
		required := false
		if op.RequestBody.Required != nil {
			required = *op.RequestBody.Required
		}
		if mt := op.RequestBody.Content.GetOrZero("application/json"); mt != nil && mt.Schema != nil {
			bodySchema, err := g.translateSchema(mt.Schema, out.GoName+"Body")
			if err != nil {
				return nil, fmt.Errorf("body: %w", err)
			}
			out.HasBody = true
			out.BodyJSON = true
			out.BodyType = bodySchema.goExpr
			out.BodyField = "Body"
			out.BodyReq = required
			out.BodyOmit = !required
			out.BodyPtr = !required && !isPointerlessKind(bodySchema)
			out.BodyDoc = cleanDoc(op.RequestBody.Description)
		} else if media := binaryRequestMedia(op.RequestBody); media != "" {
			out.HasBody = true
			out.BodyStream = true
			out.BodyMedia = media
			out.BodyType = "io.Reader"
			out.BodyField = "Body"
			out.BodyReq = required
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
		var streamCode string
		if !out.HasResult {
			// No decodable JSON response: the lowest 2xx declaring any
			// non-JSON media type (e.g. text/csv, application/octet-stream,
			// with or without a schema) streams its raw body to the caller.
			for rPair := op.Responses.Codes.First(); rPair != nil; rPair = rPair.Next() {
				code := rPair.Key()
				if !isSuccess(code) {
					continue
				}
				r := rPair.Value()
				if r == nil || r.Content == nil {
					continue
				}
				for mPair := r.Content.First(); mPair != nil; mPair = mPair.Next() {
					media := mediaTypeName(mPair.Key())
					if isJSONMedia(media) {
						continue
					}
					if streamCode == "" || code < streamCode {
						streamCode = code
						out.ResultMedia = media
					}
					break
				}
			}
			if streamCode != "" {
				out.ResultStream = true
			}
		}

		// Response headers: `headers:` declared on the primary success
		// response — the one the body comes from, or the lowest 2xx for
		// body-less operations — become a typed <Op>ResponseHeaders
		// struct returned alongside the body.
		headerCode := bestCode
		if !out.HasResult {
			headerCode = streamCode
		}
		if headerCode == "" {
			for rPair := op.Responses.Codes.First(); rPair != nil; rPair = rPair.Next() {
				code := rPair.Key()
				if !isSuccess(code) {
					continue
				}
				if headerCode == "" || code < headerCode {
					headerCode = code
				}
			}
		}
		if headerCode != "" {
			if err := g.collectResponseHeaders(out, op.Responses.Codes.GetOrZero(headerCode)); err != nil {
				return nil, fmt.Errorf("response %s: %w", headerCode, err)
			}
		}
	}

	return out, nil
}

// collectResponseHeaders translates a success response's declared
// headers into respHeaderFields on op, sorted alphabetically by Go name
// (deterministic and independent of spec order, like header params).
func (g *generator) collectResponseHeaders(op *operation, r *v3.Response) error {
	if r == nil || r.Headers == nil {
		return nil
	}
	for hPair := r.Headers.First(); hPair != nil; hPair = hPair.Next() {
		name := hPair.Key()
		// Per OpenAPI 3, a "Content-Type" entry in a response headers
		// map is ignored.
		if strings.EqualFold(name, "Content-Type") {
			continue
		}
		h := hPair.Value()
		if h == nil {
			continue
		}
		schema, err := g.translateSchema(h.Schema, op.GoName+pascal(name))
		if err != nil {
			return fmt.Errorf("header %s: %w", name, err)
		}
		goType, parse := headerFieldType(schema)
		op.RespHeaders = append(op.RespHeaders, &respHeaderField{
			GoName:   pascal(name),
			WireName: name,
			GoType:   goType,
			Parse:    parse,
			Doc:      cleanDoc(h.Description),
		})
	}
	if len(op.RespHeaders) == 0 {
		return nil
	}
	sort.Slice(op.RespHeaders, func(i, j int) bool {
		return op.RespHeaders[i].GoName < op.RespHeaders[j].GoName
	})
	op.HeadersName = op.GoName + "ResponseHeaders"
	return nil
}

// headerFieldType maps a header's schema type onto a struct field type
// and a decode strategy. Headers are strings on the wire, so anything
// beyond the primitives (and named string enums) falls back to the raw
// header string.
func headerFieldType(t *goType) (goExpr, parse string) {
	switch t.Kind {
	case kindEnum:
		return t.goExpr, "cast"
	case kindPrimitive:
		switch t.goExpr {
		case "int32", "int64", "float32", "float64", "bool":
			return t.goExpr, t.goExpr
		}
	}
	return "string", "string"
}

// binaryRequestMedia returns the first request-body media type that
// should be streamed as raw bytes: application/octet-stream, or any
// non-JSON media type whose schema is `type: string, format: binary`.
// Returns "" when the body declares no binary content.
func binaryRequestMedia(rb *v3.RequestBody) string {
	for pair := rb.Content.First(); pair != nil; pair = pair.Next() {
		media := mediaTypeName(pair.Key())
		if isJSONMedia(media) {
			continue
		}
		mt := pair.Value()
		if media == "application/octet-stream" || (mt != nil && isBinarySchema(mt.Schema)) {
			return media
		}
	}
	return ""
}

// isBinarySchema reports whether a schema is `type: string, format:
// binary` — OpenAPI's way of saying "raw bytes, not text".
func isBinarySchema(proxy *base.SchemaProxy) bool {
	if proxy == nil {
		return false
	}
	schema := proxy.Schema()
	if schema == nil {
		return false
	}
	primary, _ := primaryType(schema.Type)
	return primary == "string" && schema.Format == "binary"
}

// mediaTypeName normalizes a content key like "application/json;
// charset=utf-8" down to its bare media type. Falls back to the
// trimmed, lowercased key when mime parsing fails.
func mediaTypeName(key string) string {
	if mt, _, err := mime.ParseMediaType(key); err == nil {
		return mt
	}
	return strings.ToLower(strings.TrimSpace(key))
}

// isJSONMedia reports whether a media type carries JSON — either
// application/json itself or a +json structured suffix (RFC 6839).
func isJSONMedia(media string) bool {
	return media == "application/json" || strings.HasSuffix(media, "+json")
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
