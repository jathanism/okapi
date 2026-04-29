package openapi

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/jathanism/okapi/internal/log"

	. "github.com/jathanism/okapi/error"
	"github.com/jathanism/okapi/request"
	"github.com/jathanism/okapi/spec"
)

// Generate the OpenApi struct from the spec.
// This operation is wrapped in sh so we can format the generated code nicely.
//go:generate sh -c "go run ./gen/gen.go && go fmt ./openapi_gen.go"

var cache = map[string]*OpenApi{}

type OpenApiSpec = spec.OpenApiSpec
type ApiClientCallable = request.ApiClientCallable

type OpenApiEndpoint func(options ...request.RequestOption) error

func (o OpenApiEndpoint) With(opts ...request.RequestOption) OpenApiEndpoint {
	// log.Trace("OpenApiEndpoint.With", "opts", opts)
	return func(options ...request.RequestOption) error {
		opts = append(opts, options...)
		return o(opts...)
	}
}

// internal holds the internal state of the OpenApi struct so we don't have to
// include it in the generated public OpenApi struct.
type internal struct {
	// orig preserves the original OpenApi object so we can return to it later,
	// i.e. if we use the .WithClient(...) method to bind endpoints to a
	// particular API client instance
	orig *OpenApi
	// spec is the OpenApi Schema parsed so we can work with it nicely
	spec *spec.OpenApiSpec
}

func (o *OpenApi) NewFromBytes(source []byte) (*OpenApi, error) {
	var err error
	if o == nil {
		o = &OpenApi{}
	}

	o.orig = o

	spec, err := (*OpenApiSpec)(nil).NewFromBytes(source)
	if err != nil {
		return nil, err
	}

	err = o.build(spec)
	if err != nil {
		return nil, err
	}

	return o, nil
}

func (o *OpenApi) NewFromSource(source string) (*OpenApi, error) {
	// Fast path to return cached OpenApi instance
	if cached, ok := cache[source]; ok {
		copied := *cached
		return &copied, nil
	}

	var err error
	if o == nil {
		o = &OpenApi{}
	}

	spec, err := (*OpenApiSpec)(nil).NewFromSource(source)
	if err != nil {
		return nil, err
	}

	err = o.build(spec)
	if err != nil {
		return nil, err
	}

	// Cache for reuse
	copied := *o
	cache[source] = &copied

	return o, nil
}

func (o *OpenApi) build(spec *spec.OpenApiSpec) error {
	o.orig = o
	o.spec = spec

	// Get the actual value of the *OpenApi pointer
	openApiElem := reflect.ValueOf(o).Elem()
	// Get the reflected Type of the OpenApiEndpoint
	endpointType := reflect.TypeOf(OpenApiEndpoint(nil))

	// Looping over all the endpoints in the spec and binding them to the
	// OpenApi generated struct.
	for _, endpoint := range o.Endpoints() {
		name := endpoint.MethodName()
		field := openApiElem.FieldByName(name)
		if !field.IsValid() {
			log.Trace("Could not find endpoint: invalid", "name", name)
			continue
		}
		if field.Type() != endpointType {
			log.Trace("Could not find endpoint: wrong type", "name", name, "type", field.Type())
			continue
		}
		fn := MakeOpenApiEndpoint(endpoint)
		field.Set(reflect.ValueOf(fn))
	}

	// Second pass to bind endpoints in the struct which aren't in the spec.
	// These endpoints are just bound with a Endpoint function that returns an
	// informative error.
	for i := 0; i < openApiElem.NumField(); i++ {
		// Get the field and its name
		field := openApiElem.Field(i)
		// Check if it's an OpenApiEndpoint
		switch field.Type() {
		case endpointType:
			// log.Trace(name)
		default:
			// Ignore non-OpenApiEndpoints
			continue
		}

		if !field.IsValid() {
			// TODO(shakefu): This shouldn't happen?
			continue
		}
		if !field.IsZero() {
			continue
		}
		// If we've got this far, set it to the NilEndpoint
		field.Set(reflect.ValueOf(OpenApiEndpoint(NilEndpoint)))
	}

	return nil
}

func (o *OpenApi) WithClient(client request.OpenApiClient) *OpenApi {
	return o.With(request.RequestJSON(client.RequestJSON))
}

func (o *OpenApi) With(options ...request.RequestOption) *OpenApi {
	// Copy our OpenApi instance, throwing away changes that may have occurred
	// since the original was instantiated
	api := *(o.orig)

	// Get the reflected value of the pointer to the OpenApi instance, so we can
	// modify it directly. If you don't get the value of the pointer first, then
	// you can't .Set field values.
	apiVal := reflect.ValueOf(&api).Elem()

	// Iterate over the fields and call the With method on the OpenApiEndpoint.
	for _, name := range o.EndpointNames() {
		field := apiVal.FieldByName(name)
		if !field.IsValid() || field.IsZero() || field.IsNil() {
			log.Trace("Could not find endpoint", "name", name)
			continue
		}

		// Get the OpenApiEndpoint.With method so we can call it with the options
		with := field.MethodByName("With")
		if !with.IsValid() {
			log.Trace("Could not get .With method on endpoint", "name", name)
			continue
		}

		wrapped := with.CallSlice([]reflect.Value{reflect.ValueOf(options)})
		if wrapped != nil && len(wrapped) == 0 {
			log.Trace("Returned no wrapped endpoint", "name", name, "with", with, "wrapped", wrapped)
			continue
		}
		field.Set(wrapped[0])
	}
	return &api
}

// Endpoints returns a map of all the endpoints in the OpenApi spec.
func (o *OpenApi) Endpoints() map[string]*spec.Endpoint {
	return o.spec.Endpoints
}

// EndpointNames returns a list of all the CamelCase endpoint names in the OpenApi struct.
func (o *OpenApi) EndpointNames() []string {
	return o.spec.MethodNames()
}

// EndpointOperationNames returns a list of all the operation names in the OpenApi spec.
func (o *OpenApi) EndpointOperationNames() []string {
	return o.spec.OperationNames()
}

// MakeOpenApiEndpoint returns an OpenApiEndpoint from a spec.Endpoint.
func MakeOpenApiEndpoint(endpoint *spec.Endpoint) OpenApiEndpoint {
	return OpenApiEndpoint(func(options ...request.RequestOption) error {
		return CallEndpoint(endpoint, options...)
	})
}

// CallEndpoint builds a request and makes an API call using the request's ApiClient.
func CallEndpoint(endpoint *spec.Endpoint, options ...request.RequestOption) error {
	// Build the request
	req := request.NewRequest(options...)
	log.Trace("NewRequest", "req", req)

	// Validate the request against the endpoint
	err := endpoint.Validate(req.Params, req.Body)
	if err != nil {
		return err
	}

	// Make our request, we throw away the raw response object
	if req.ApiClient == nil {
		return Error("No ApiClient available, did you forget to call OpenApi.WithClient()?")
	}

	// Capitalize the method, which is wasteful to always do but meh
	method := strings.ToUpper(endpoint.Method)

	// Check for parameters that should be headers and move them
	for name, values := range req.Params {
		param, ok := endpoint.Params[name]
		if !ok {
			continue
		}
		if param.In == "header" {
			// Convert the param to a header
			for _, val := range values {
				if str, ok := val.(string); ok {
					req.Headers[name] = append(req.Headers[name], str)
				} else {
					req.Headers[name] = append(req.Headers[name], fmt.Sprint(val))
				}
			}
			// Remove it from params so it doesn't get added to URL
			delete(req.Params, name)
		}
	}

	// Make our URL, this should never fail since it's validated
	url := endpoint.MustMakeUrl(req.Params)

	// Make the actual API call
	resp, err := req.ApiClient(method, url, req.Json(), req.Result, req.Headers)

	log.Trace("OpenApiEndpoint response", "err", err, "resp", resp)

	return err
}

// NilEndpoint is an OpenApiEndpoint that returns an error when called.
func NilEndpoint(options ...request.RequestOption) error {
	return Error("Endpoint not present in OpenApi Schema")
}
