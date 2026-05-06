package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/jathanism/okapi/internal/log"
	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	. "github.com/jathanism/okapi/error"
)

type OpenApiSpec struct {
	// Source is the OpenAPI specification schema. It can be raw JSON or YAML, a
	// file:// URL, or an HTTP URL
	Source string
	// RawSchema is the raw bytes of the OpenAPI schema
	RawSchema []byte

	Endpoints map[string]*Endpoint
	// endpointMethodNames caches the CamelCase method names for each endpoint
	endpointMethodNames []string
	// endpointOperationNames caches snake_case operation names for each endpoint
	endpointOperationNames []string
	// endpointMethodToOperation caches the mapping between method names and operations
	endpointMethodToOperation map[string]string
}

// Endpoint represents an API endpoint.
type Endpoint struct {
	// Name is the name of the endpoint.
	Name string
	// Summary is a brief summary of the endpoint.
	Summary string
	// Description is a detailed description of the endpoint.
	Description string
	// Method is the HTTP method of the endpoint.
	Method string
	// Path is the URL path of the endpoint.
	Path string

	// Params is a map of parameters that the endpoint accepts. The key is the name
	// of the parameter, and the value is the parameter itself.
	Params map[string]*Param
	// Body is the request body of the endpoint, if any.
	Body *Body

	// methodName is the CamelCase method name of the endpoint.
	methodName string
}

// Param represents a parameter of an API endpoint.
type Param struct {
	// Name is the name of the parameter.
	Name string
	// In is the location of the parameter. It can be "query", "path", "header",
	// or "cookie".
	In string
	// Type is the data type of the parameter.
	Type string
	// Description is a brief description of the parameter.
	Description string
	// Required is a boolean indicating whether the parameter is required.
	Required bool
}

// Body represents the request body of an API endpoint.
type Body struct {
	// MapSchema is the JSON schema for the request body in map format.
	MapSchema map[string]any
	// JsonSchema is the JSON schema for the request body in as a parsed jsonschema validator.
	JsonSchema *jsonschema.Schema
	// Yaml is the YAML representation of the request body schema.
	Yaml string
	// Json is the JSON representation of the request body schema.
	Json string
}

var cache = map[string]*OpenApiSpec{}

/* OpenApiSpec methods
**********************/

// NewFromBytes builds a new OpenApiSpec from the provided schema encoded as
// bytes. This method does not provide caching.
func (o *OpenApiSpec) NewFromBytes(source []byte) (*OpenApiSpec, error) {
	if o != nil {
		return nil, Error("already initialized, please use a nil instance")
	}

	o = &OpenApiSpec{Source: string(source)}
	err := o.New()
	if err != nil {
		return nil, err
	}

	return o, nil
}

// NewFromSource builds a new OpenApiSpec from the provided source. The source
// can be a file:// URL or an HTTP URL or raw bytes. This method will cache the
// resulting OpenApiSpec, and return the same instance for the exact same
// source.
func (o *OpenApiSpec) NewFromSource(source string) (*OpenApiSpec, error) {
	if o != nil {
		return nil, Error("already initialized, please use a nil instance")
	}

	// Fast path to return cached OpenApiSpec instance
	if cached, ok := cache[source]; ok {
		return cached, nil
	}

	o = &OpenApiSpec{Source: source}
	err := o.New()
	if err != nil {
		return nil, err
	}

	cache[source] = o

	return o, nil
}

// New builds a new OpenApiSpec from the provided source. [OpenApiSpec.Source]
// must be set on the object before calling this method.
func (o *OpenApiSpec) New() (err error) {
	if o.Source == "" {
		return Error("no source provided")
	}

	data, err := o.ReadSource()
	if err != nil {
		return err
	}

	// Save the raw schema for later
	o.RawSchema = data

	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return Error("Could not parse OpenApi file", err)
	}

	api, errs := doc.BuildV3Model()
	if errs != nil {
		return Error("Could not parse OpenApi file", errs)
	}

	// All the URL paths we have
	allPaths := api.Index.GetAllPaths()

	// Temporary wrapper to make it easier to iterate
	type Operation struct {
		method string
		path   string
		item   *v3.PathItem
		op     *v3.Operation
	}
	operations := make([]Operation, 0,
		len(allPaths)*4, // This is just a ceiling of the number of operations
	)

	// Get all the operations
	for path, pathMethods := range api.Index.GetAllPaths() {
		for method := range pathMethods {
			item := api.Model.Paths.PathItems.GetOrZero(path)
			if item == nil {
				log.Debug("Could not find path", "path", path)
				continue
			}
			op := item.GetOperations().GetOrZero(method)
			if op == nil {
				log.Debug("Could not find operation", "path", path, "method", method)
				continue
			}
			operations = append(operations, Operation{
				method: method,
				path:   path,
				item:   item,
				op:     op,
			})
		}
	}

	// Allocate our endpoints
	o.Endpoints = make(map[string]*Endpoint, len(operations))
	// And our name caches
	o.endpointMethodNames = make([]string, 0, len(operations))
	o.endpointOperationNames = make([]string, 0, len(operations))
	o.endpointMethodToOperation = make(map[string]string, len(operations))

	for _, op := range operations {
		endpoint := MakeEndpoint(op.method, op.path, op.op, op.item)
		o.Endpoints[endpoint.Name] = endpoint
		o.endpointMethodNames = append(o.endpointMethodNames, endpoint.MethodName())
		o.endpointOperationNames = append(o.endpointOperationNames, endpoint.Name)
		o.endpointMethodToOperation[endpoint.MethodName()] = endpoint.Name
	}

	sort.Strings(o.endpointMethodNames)
	sort.Strings(o.endpointOperationNames)

	return
}

// ReadSource reads the source file specified by the OpenAPI struct's Source field.
// It handles file:// URLs and will eventually support other protocols.
//
// If the Source field does not contain a protocol (e.g., "file://"), it is assumed to be a local file path.
//
// If the Source field contains a protocol but it's not "file://", an error is returned.
//
// The function returns the contents of the file as a byte slice and any encountered errors.
//
// Example:
//
//	openapi := &OpenAPI{Source: "file:///path/to/source.yaml"}
//	data, err := openapi.ReadSource()
//	if err != nil {
//	    log.Fatal(err)
//	}
func (o *OpenApiSpec) ReadSource() ([]byte, error) {
	if o.Source == "" {
		return nil, Error("no source provided")
	}

	// Handle file:// uris for local and testing
	if strings.HasPrefix(o.Source, "file://") {
		source := strings.TrimPrefix(o.Source, "file://")
		b, err := os.ReadFile(source)
		if err != nil {
			return nil, Error(err)
		}
		return b, nil
	}

	// Handle http:// and https:// uris for remote sources
	if strings.HasPrefix(o.Source, "http://") || strings.HasPrefix(o.Source, "https://") {
		return getHttpSource(o.Source)
	}

	// Fallback to treating o.Source as a schema
	return []byte(o.Source), nil
}

// getHttpSource gets the API schema from an HTTP source.
func getHttpSource(source string) ([]byte, error) {
	// Try to get the schema URL
	resp, err := http.Get(source)
	if err != nil {
		return nil, Error(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// TODO(shakefu): This maybe should just log and exit safely, instead of erroring
		return nil, Errorf("Unexpected status code: %d", resp.StatusCode)
	}

	// Read the body
	schema, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, Error(err)
	}

	return schema, nil
}

// MethodNames the CamelCase method names for each endpoint.
func (o *OpenApiSpec) MethodNames() []string {
	return o.endpointMethodNames
}

// OperationNames snake_case operation names for each endpoint.
func (o *OpenApiSpec) OperationNames() []string {
	return o.endpointOperationNames
}

func (o *OpenApiSpec) GetOperationName(methodName string) string {
	return o.endpointMethodToOperation[methodName]
}

/* OpenApiSpec helper functions
*******************************/

// MakeEndpoint creates an endpoint from an OpenAPI operation.
func MakeEndpoint(method, path string, op *v3.Operation, item *v3.PathItem) *Endpoint {
	e := &Endpoint{
		Name:        op.OperationId,
		Summary:     op.Summary,
		Description: op.Description,
		Method:      method,
		Path:        path,
		Params:      make(map[string]*Param, len(op.Parameters)),
		Body:        new(Body),
	}

	// Iterate over all the parameters, saving them for later use
	for i, param := range op.Parameters {
		name := param.Name
		if name == "" {
			name = fmt.Sprintf("param%d", i)
		}
		required := false
		if param.Required != nil {
			required = *param.Required
		}
		// TODO(shakefu): Params of type "array" in the query string can be
		// expanded with "?expand=<param>", but we don't support that yet
		e.Params[name] = &Param{
			Name:        name,
			In:          param.In,
			Type:        first(param.Schema.Schema().Type),
			Description: param.Description,
			Required:    required,
		}
	}

	// The request body is optional, but if it's present, we need to save it
	if op.RequestBody == nil {
		return e
	}
	if op.RequestBody.Content == nil {
		return e
	}
	requestBody := op.RequestBody.Content.GetOrZero("application/json")
	if requestBody == nil {
		return e
	}

	// Retrieve the Schema object representing the request body's payload
	bodySchema := requestBody.Schema.Schema()
	if bodySchema == nil {
		return e
	}

	// We have to render it to YAML ... to convert it to a map ... to convert it
	// to JSON ... to be able to use a JSON schema validator
	yamlSchema, err := bodySchema.Render()
	if err != nil {
		return e
	}
	e.Body.Yaml = string(yamlSchema)

	schema := make(map[string]any)
	err = yaml.Unmarshal(yamlSchema, &schema)
	if err != nil {
		return e
	}

	// We need to check if any of the required fields are read-only in which
	// case they shouldn't be required
	required := bodySchema.Required
	actuallyRequired := make([]string, 0, len(required))
	for _, name := range required {
		prop := bodySchema.Properties.GetOrZero(name)
		if prop == nil {
			// Assume if we can't find it, that it's required
			actuallyRequired = append(actuallyRequired, name)
			continue
		}
		propSchema := prop.Schema()
		if propSchema == nil {
			// Assume if we can't get a schema it's required
			actuallyRequired = append(actuallyRequired, name)
			continue
		}
		if propSchema.ReadOnly == nil {
			// Assume if the ReadOnly property is nil, it's required
			actuallyRequired = append(actuallyRequired, name)
			continue
		}
		if !(*propSchema.ReadOnly) {
			// If it's not read-only, it's required
			actuallyRequired = append(actuallyRequired, name)
		}
	}
	schema["required"] = actuallyRequired
	e.Body.MapSchema = schema

	// Convert it to JSON to use the JSON schema validator
	bodyJson, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return e
	}
	e.Body.Json = string(bodyJson)

	// Build the validator
	compiler := jsonschema.NewCompiler()
	compiler.AddResource(e.Name, bytes.NewReader(bodyJson))
	jsonSchema, _ := compiler.Compile(e.Name)
	e.Body.JsonSchema = jsonSchema

	return e
}

/* Endpoint methods
*******************/

// camelCaseBoundary matches a lowercase letter or digit immediately followed
// by an uppercase letter, which is the boundary between words in a camelCase
// or PascalCase identifier (e.g. the "kA" in "ackAudience").
var camelCaseBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// wordSeparator splits a normalized identifier on _ or - runs.
var wordSeparator = regexp.MustCompile(`[_-]+`)

// MethodName returns the CamelCase method name for the endpoint.
//
// This converts _, -, and camelCase/PascalCase boundaries into words that are
// joined as CamelCase. For example, "accounts_verify-email" and
// "accountsVerifyEmail" both become "AccountsVerifyEmail".
func (e *Endpoint) MethodName() string {
	if e.methodName != "" {
		return e.methodName
	}

	// Insert underscores at camelCase/PascalCase boundaries so the splitter
	// below treats "ackAudience" the same as "ack_audience". Without this,
	// the previous lowercase-only regex silently dropped every uppercase
	// letter that started an internal word.
	normalized := camelCaseBoundary.ReplaceAllString(e.Name, "${1}_${2}")

	parts := wordSeparator.Split(normalized, -1)
	out := parts[:0]
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, strings.ToUpper(part[0:1])+strings.ToLower(part[1:]))
	}
	e.methodName = strings.Join(out, "")
	return e.methodName
}

func (e *Endpoint) String() string {
	return fmt.Sprint(e.Name, ":", e.Method, " ", e.Path, " - ", e.ParamNames())
}

func (e *Endpoint) ParamNames() []string {
	var names []string
	for name := range e.Params {
		names = append(names, name)
	}
	return names
}

// HasRequestBody returns true if the endpoint has a request body.
func (e *Endpoint) HasRequestBody() bool {
	return e.Body.JsonSchema != nil
}

// BodySchema returns the JSON schema for the endpoint's request body.
func (e *Endpoint) BodySchema(prefixes ...string) string {
	var prefix string
	if len(prefixes) > 0 {
		prefix = prefixes[0]
	}
	if e.Body == nil {
		return ""
	}
	if e.Body.MapSchema == nil {
		return ""
	}
	properties, ok := e.Body.MapSchema["properties"]
	if !ok {
		return ""
	}
	data, _ := json.MarshalIndent(properties, prefix, "  ")
	if data == nil {
		return ""
	}
	return string(data)
}

// Validate checks the supplied params, headers, and body against the
// endpoint's declared schema.
//
// Spec-declared header parameters MUST be supplied via headers (i.e.
// request.Header(...)), not via params (request.Param(...)). Passing a
// spec-declared header parameter through params returns an error pointing
// the caller at the right helper. Headers that are not declared in the spec
// are passed through untouched (e.g. ad-hoc Authorization, Content-Type).
func (e *Endpoint) Validate(params map[string][]any, headers map[string][]string, body any) error {

	// Type validation for non-header params. Header params declared in the
	// spec must come in through `headers`, not `params` — flag that mistake
	// with a clear message instead of silently moving them like we used to.
	for name, values := range params {
		param, ok := e.Params[name]
		if !ok {
			return Error(OpenApiValidationError, "Unknown parameter: "+name)
		}
		if param.In == "header" {
			return Error(OpenApiValidationError, fmt.Sprintf(
				"Parameter %s is a header — pass it with request.Header(%q, ...) instead of request.Param(...)",
				name, name,
			))
		}
		for _, value := range values {
			err := param.Validate(value)
			if err != nil {
				return Error(OpenApiValidationError, err)
			}
		}
	}

	// Type validation for header params declared in the spec. Headers not
	// declared in the spec are pass-through and are not validated here.
	for name, values := range headers {
		param, ok := e.Params[name]
		if !ok {
			continue
		}
		if param.In != "header" {
			return Error(OpenApiValidationError, fmt.Sprintf(
				"Parameter %s is a %s parameter — pass it with request.Param(%q, ...) instead of request.Header(...)",
				name, param.In, name,
			))
		}
		for _, value := range values {
			err := param.Validate(value)
			if err != nil {
				return Error(OpenApiValidationError, err)
			}
		}
	}

	// Presence validation
	for name, param := range e.Params {
		if !param.Required {
			continue
		}
		if param.In == "header" {
			if _, ok := headers[name]; !ok {
				return Error(OpenApiValidationError, "Header "+name+" is required")
			}
			continue
		}
		if _, ok := params[name]; !ok {
			return Error(OpenApiValidationError, "Parameter "+name+" is required")
		}
	}

	if e.Body == nil { // This can't happen, but it's safer, yay?
		if body != nil {
			// TODO(shakefu): This might not be always true?
			return Error(OpenApiValidationError, "Endpoint does not take a request body")
		}
	} else {
		err := e.Body.Validate(body)
		if err != nil {
			return Error(OpenApiValidationError, err)
		}
	}

	return nil
}

func (e *Endpoint) MustMakeUrl(params map[string][]any) string {
	segments := strings.Split(e.Path, "/")

	// Parsing/validating the URL path presence
	for i, segment := range segments {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
			param, ok := e.Params[name]
			if !ok {
				// This shouldn't happen
				panic(fmt.Sprintf("Unknown param: %s", name))
			}
			if values, ok := params[name]; ok {
				for _, value := range values {
					val := param.Coerce(value)
					switch val.(type) {
					case string:
						segments[i] = val.(string)
					default:
						panic(val)
					}
				}
			} else {
				panic(fmt.Sprintf("Missing param: %s", name))
			}
		}
	}
	path := strings.Join(segments, "/")

	// Parse the URL to make a url.URL object we can add params to
	url, _ := url.Parse(path)
	query := url.Query()

	// Iterate over the known params and add them to the querystirng
	for name, paramDef := range e.Params {
		if paramDef.In != "query" {
			continue
		}
		if values, ok := params[name]; ok {
			for _, value := range values {
				val := paramDef.Coerce(value)
				switch val.(type) {
				case string:
					log.Trace("Adding param", "name", name, "value", value)
					query.Add(name, val.(string))
				case []string:
					for _, v := range val.([]string) {
						log.Trace("Adding param", "name", name, "value", v)
						query.Add(name, v)
					}
				default:
					panic(val)
				}
			}
		}
	}
	url.RawQuery = query.Encode()

	log.Trace("Making URL", "url", fmt.Sprintf("%#v", url))

	return url.String()
}

/* Param methods
****************/

func (p *Param) Validate(value any) error {
	if value == nil {
		return Error(OpenApiValidationError, fmt.Errorf("parameter %s cannot be nil", p.Name))
	}
	err := Validate(value, p.Type)
	if err != nil {
		return Error(OpenApiValidationError, fmt.Errorf("parameter %s: %w", p.Name, err))
	}
	return nil
}

func (p *Param) Coerce(value any) any {
	if value == nil {
		return Errorf("parameter %s cannot be nil", p.Name)
	}
	// Default to string type if none is set
	paramType := p.Type
	if paramType == "" {
		paramType = "string"
	}
	var err = Errorf("parameter %s must be %s", p.Name, p.Type)

	// All path and query params must be converted to strings
	if p.In == "path" || p.In == "query" {
		switch value.(type) {
		case string:
			return value
		case []string:
			return value
		case []byte:
			return string(value.([]byte))
		case byte:
			return fmt.Sprintf("%c", value)
		case int, int16, int32, int64, uint, uint16, uint32, uint64:
			return fmt.Sprint(value)
		case float32, float64:
			return fmt.Sprint(value)
		case bool:
			return fmt.Sprint(value)
		default:
			return err
		}
	}

	return Errorf("unsupported parameter type: %s", p.Type)
}

/* Body methods
***************/

func (b *Body) Validate(value any) error {
	if value == nil && b.JsonSchema != nil {
		return Errorf("body cannot be nil")
	}
	if b.JsonSchema == nil {
		// We don't have a parsed schema, so we can't validate, let it fly
		return nil
	}

	// Special handling for CSV data - we don't validate it at this stage
	// since it will be converted to JSON format later
	switch value.(type) {
	case []byte:
		// Check if it looks like CSV data (contains commas and newlines)
		data := string(value.([]byte))
		if strings.Contains(data, ",") && strings.Contains(data, "\n") {
			return nil
		}
	case string:
		if strings.Contains(value.(string), ",") && strings.Contains(value.(string), "\n") {
			return nil
		}
	}

	var err error
	// Try to blindly validate the value, it might work if happens to match the Schema
	err = b.JsonSchema.Validate(value)
	if err == nil {
		return nil
	}
	// This is just a straight up validation error, it parsed and compared
	// successfully and failed
	if strings.Contains(err.Error(), "doesn't validate with ") {
		log.Trace("Object doesn't validate:", "err", err.Error())
		return err
	}

	// This happens if we stuff in a random struct, which means we need to marshal it
	if strings.Contains(err.Error(), "jsonschema: invalid jsonType") {
		log.Trace("Invalid jsonType", "err", err.Error())
		switch value.(type) {
		case []byte, string:
		default:
			value, err = json.Marshal(value)
			if err != nil {
				return err
			}
		}
	} else if !strings.Contains(err.Error(), ": expected object, but got") {
		log.Trace("Expected object", "err", err.Error())
		// Try to coerce the value to a string if we aren't expecting an object...
		// this is a bit of a hack, but the error message looks like: "jsonschema:
		// '' does not validate with file:///...#/type: expected object, but got
		// string", so we can check against it
		value = fmt.Sprint(value)
		err = b.JsonSchema.Validate(value)
		if err != nil {
			return err
		}
		return nil
	}

	// At this point we probably expect an object
	var obj any

	log.Trace("Object validation", "value", string(value.([]byte)), "type", reflect.TypeOf(value))

	// Try to unmarshal the string
	switch value.(type) {
	case string:
		err = json.Unmarshal([]byte(value.(string)), &obj)
	case []byte:
		err = json.Unmarshal(value.([]byte), &obj)
		if err != nil {
			log.Trace("Failed to unmarshal []byte", "err", err)
		}
	default:
		obj = value
		err = nil
	}
	if err != nil {
		log.Trace("Failed to unmarshal", "err", err, "value", value, "type", reflect.TypeOf(value))
		return err
	}

	log.Trace("Unmarshalled object", "obj", obj)

	// And try to validate it
	err = b.JsonSchema.Validate(obj)
	if err != nil {
		return err
	}

	// If we're here, it's valid
	return nil
}

/* Generic helper functions
***************************/

func Validate(value any, paramType string) error {
	log.Trace("Validating", "value", value, "paramType", paramType)
	// TODO(shakefu): Revisit this to see if it makes sense
	// If there's no Type provided, we assume it's a string
	if paramType == "" {
		paramType = "string"
	}

	if paramType == "object" {
		// TODO(shakefu): This might just be better to see if it does json.Marshal()
		_, ok := value.(map[string]any)
		if !ok {
			return Errorf("Invalid type, want %s, got: %T", paramType, value)
		}
		return nil
	}

	// Trivial check against Type
	switch value.(type) {
	case string, []byte, byte:
		if paramType == "string" {
			return nil
		}
	case float32, float64:
		log.Trace("Validating float32", "value", value)
		if paramType == "number" {
			return nil
		}
	case bool:
		log.Trace("Validating bool", "value", value)
		if paramType == "boolean" {
			return nil
		}
	case int, int16, int32, int64, uint, uint16, uint32, uint64:
		log.Trace("Validating int", "value", value)
		if paramType == "integer" {
			return nil
		}
	case []string:
		log.Trace("Validating array(string)", "value", value)
		if paramType == "array" {
			return nil
		}
	case []any:
		log.Trace("Validating array", "value", value)
		if paramType == "array" {
			return nil
		}
	default:
		return Errorf("Invalid type, want %s, got: %T", paramType, value)
	}
	return nil
}

// first returns the first string from the slice if it's not empty,
// otherwise it returns an empty string.
//
// Example:
//
//	s := []string{"string1", "string2", "string3"}
func first[T any](s []T) T {
	if len(s) == 0 {
		var zero T
		return zero
	}
	return s[0]
}
