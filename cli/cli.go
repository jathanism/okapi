package cli

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/jathanism/okapi"
	"github.com/jathanism/okapi/internal/log"
	"github.com/jathanism/okapi/request"
	"github.com/jathanism/okapi/spec"
)

// CliContext defines the runtime context needed by OpenApiCli.
// Consumers implement this to bridge their application context into Okapi.
type CliContext interface {
	Stdin() *os.File
	Stdout() io.Writer
	Host() string
	SetHost(host string)
	GetApiClient() request.OpenApiClient
	EmitJsonObj(obj any) error
	Error(msg string) error
}

// OpenApiCli is a wrapper around the cli.App which allows it to integrate with
// the base CLI commands of the consuming application.
type OpenApiCli struct {
	Api      *openapi.OpenApi
	App      *cli.App
	client   request.OpenApiClient
	ctx      CliContext
	AppName  string // CLI binary name (e.g. "okapi")
	DocsURL  string // Base URL for documentation links
	HostFlag cli.Flag
}

// DefaultHostFlag returns the default --host flag configuration.
// Override by setting OpenApiCli.HostFlag before calling New().
func DefaultHostFlag(defaultHost string, envVar string) cli.Flag {
	return &cli.StringFlag{
		Name:    "host",
		Aliases: []string{"H"},
		Usage:   "The host to connect to.",
		Value:   defaultHost,
		EnvVars: []string{envVar},
	}
}

// New creates a new OpenApiCli
func (o *OpenApiCli) New() *OpenApiCli {
	if o == nil {
		panic("OpenApiCli cannot New from nil")
	}

	if o.Api == nil {
		panic("Api is required")
	}

	appName := o.AppName
	if appName == "" {
		appName = "okapi"
	}

	// Use the default host flag if none was provided
	hostFlag := o.HostFlag
	if hostFlag == nil {
		hostFlag = DefaultHostFlag("", "OKAPI_HOST")
	}

	// Create the top level command instance
	if o.App == nil {
		o.App = &cli.App{
			Name:            appName,
			HideHelpCommand: true,
			HideVersion:     true,
			Flags:           []cli.Flag{hostFlag},
		}
	}

	// Bind the commands to the app
	o.App.Commands = o.makeCommandTree(o.Api)

	return o
}

// Run runs the OpenApiCli
func (o *OpenApiCli) Run(ctx CliContext, args []string) error {
	// Get our context object and bind it to the OpenApiCli
	if ctx == nil {
		panic("ctx cannot be nil")
	}
	o.ctx = ctx

	// Bind the Stdin/Stdout objects to the OpenApiCli
	o.App.Reader = ctx.Stdin()
	o.App.Writer = ctx.Stdout()

	return o.App.Run(args)
}

// Commands returns the commands in the OpenApiCli
func (o *OpenApiCli) Commands() []*cli.Command {
	return o.App.Commands
}

// makeAction creates an action function for the given endpoint which will be
// called when the command is executed.
func (o *OpenApiCli) makeAction(endpoint *spec.Endpoint) cli.ActionFunc {
	return func(cCtx *cli.Context) error {
		ctxs := cCtx.Lineage()
		for _, ctx := range ctxs {
			host := ctx.String("host")
			if host != "" && host != o.AppName {
				log.Trace("Connecting to", "host", host)
				o.ctx.SetHost(host)
				break
			}
			log.Trace("Skipping context", "host", host)
		}

		if o.client == nil {
			o.client = o.ctx.GetApiClient()
		}

		// Iterate over params getting each flag if it's set
		opts := make([]request.RequestOption, 0, len(endpoint.Params))
		opts = append(opts, request.WithClient(o.client))

		for name, param := range endpoint.Params {
			if !cCtx.IsSet(name) {
				continue
			}
			var paramFromFlag request.RequestOption
			switch param.Type {
			case "string":
				paramFromFlag = request.Param(name, cCtx.String(name))
			case "integer":
				paramFromFlag = request.Param(name, cCtx.Int(name))
			case "boolean":
				paramFromFlag = request.Param(name, cCtx.Bool(name))
			case "number":
				paramFromFlag = request.Param(name, cCtx.Float64(name))
			case "array":
				paramFromFlag = request.Param(name, cCtx.StringSlice(name))
			default:
				log.Debug("default param", "name", name, "type", param.Type, "value", cCtx.String(name))
			}

			opts = append(opts, paramFromFlag)
		}

		// If we have a request body schema, we capture and parse the --data
		// flag
		if endpoint.HasRequestBody() {
			bodyValue := cCtx.String("data")
			data, err := o.parseRequestBodyData(cCtx.App.Reader, bodyValue)
			if err != nil {
				return err
			}

			opts = append(opts, request.Body(data))
		}

		var result any
		opts = append(opts, request.Result(&result))

		// Make our request options obj
		req := request.RequestOptions(opts...)

		// Get our endpoint callable
		apiCall := openapi.MakeOpenApiEndpoint(endpoint)

		// Call the API
		err := apiCall(req)
		if err != nil {
			return err
		}
		if result != nil {
			o.ctx.EmitJsonObj(result)
		}
		return nil
	}
}

// parseRequestBodyData parses the request body data from stdin or a file path or a string
func (o *OpenApiCli) parseRequestBodyData(stdin io.Reader, bodyValue string) ([]byte, error) {
	if bodyValue == "" {
		return nil, o.ctx.Error("No data provided")
	}

	// Read from stdin if the body value is "-"
	if bodyValue == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, o.ctx.Error(err.Error())
		}
		return data, nil
	}

	// Try to use bodyValue as a path to a JSON file and open it
	path, err := filepath.Abs(bodyValue)
	if err == nil {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				switch err.(type) {
				case *os.PathError:
				default:
					log.Trace("parseRequestBodyData", "type", fmt.Sprintf("%T", err), "err", err)
					return nil, err
				}
			}
		} else {
			return data, nil
		}
	}

	return []byte(bodyValue), nil
}

// makeCommandTree creates a command tree for the given OpenApi object
func (o *OpenApiCli) makeCommandTree(api *openapi.OpenApi) []*cli.Command {
	// cmds := make([]*cli.Command, 0, 16) // 16 is a good number for the number of base commands
	cmds := make(map[string]*cli.Command)
	endpoints := api.Endpoints()
	for _, name := range api.EndpointOperationNames() {
		endpoint := endpoints[name]
		parts := strings.Split(endpoint.Name, "_")
		verb := parts[0]
		if cmds[verb] == nil {
			cmds[verb] = &cli.Command{
				Name:            verb,
				Usage:           "Manage " + verb + ".",
				HideHelpCommand: true,
				Flags:           []cli.Flag{o.getHostFlag()},
			}
		}
		o.makeSubcommands(cmds[verb], parts[1:], endpoint)
	}
	cmdList := make([]*cli.Command, 0, len(cmds))
	for _, c := range cmds {
		cmdList = append(cmdList, c)
	}
	return cmdList
}

// getHostFlag returns the configured host flag, or a default one.
func (o *OpenApiCli) getHostFlag() cli.Flag {
	if o.HostFlag != nil {
		return o.HostFlag
	}
	return DefaultHostFlag("", "OKAPI_HOST")
}

// makeSubcommands creates a subcommand tree from the given endpoint and parts
func (o *OpenApiCli) makeSubcommands(parent *cli.Command, parts []string, endpoint *spec.Endpoint) *cli.Command {
	if len(parts) < 1 {
		panic("MustMakeSubcommands called with no parts")
	}

	if parent.Subcommands == nil {
		parent.Subcommands = make([]*cli.Command, 0)
	}

	if len(parts) == 1 {
		extraDescription := ""
		if bodySchema := endpoint.BodySchema(); bodySchema != "" {
			if !strings.HasSuffix(endpoint.Description, "\n\n") {
				extraDescription = "\n\n"
			} else if !strings.HasSuffix(bodySchema, "\n") {
				extraDescription = "\n"
			}
			extraDescription += "Schema for JSON request body (--data):\n\n```json\n" + bodySchema + "\n```"
		}

		// We have a leaf
		cmd := &cli.Command{
			Name:        parts[0],
			Usage:       endpoint.Summary,
			Description: endpoint.Description + extraDescription,
			// Category:        parent.Name,
			HideHelpCommand: true,
			Action:          o.makeAction(endpoint),
			Flags:           makeFlags(endpoint, o.getHostFlag()),
		}
		parent.Subcommands = append(parent.Subcommands, cmd)
		return parent
	}

	verb := parts[0]

	// Search for an existing matching verb
	for _, subcmd := range parent.Subcommands {
		if subcmd.Name == verb {
			return o.makeSubcommands(subcmd, parts[1:], endpoint)
			// return subcmd
		}
	}

	// No matching verb, make a new one
	// TODO(shakefu): Figure out how to add good Usage/Description
	cmd := &cli.Command{
		Name:            verb,
		Usage:           "Manage " + verb,
		HideHelpCommand: true,
	}
	parent.Subcommands = append(parent.Subcommands, cmd)
	return o.makeSubcommands(cmd, parts[1:], endpoint)
	// return cmd
}

// makeFlags creates a list of cli.Flags from a spec.Endpoint
func makeFlags(endpoint *spec.Endpoint, hostFlag cli.Flag) []cli.Flag {
	flags := make([]cli.Flag, 0, len(endpoint.Params)+1)
	flags = append(flags, hostFlag)
	for name, param := range endpoint.Params {
		flags = append(flags, makeFlag(name, param))
	}

	// If we have a request body schema, we add a required --data flag
	if endpoint.HasRequestBody() {
		flags = append(flags, &cli.StringFlag{
			Name:     "data",
			Usage:    "[required] Data for the request body. May be a path, JSON string, or \"-\" for stdin (default: \"-\").",
			Value:    "-",
			Required: true,
		})
	}
	return flags
}

// makeFlag creates a cli.Flag from a spec.Param
func makeFlag(name string, param *spec.Param) cli.Flag {
	switch param.Type {
	case "string":
		return &cli.StringFlag{
			Name:     name,
			Usage:    param.Description,
			Required: param.Required,
		}
	case "integer":
		return &cli.IntFlag{
			Name:     name,
			Usage:    param.Description,
			Required: param.Required,
		}
	case "number":
		return &cli.Float64Flag{
			Name:     name,
			Usage:    param.Description,
			Required: param.Required,
		}
	case "boolean":
		return &cli.BoolFlag{
			Name:     name,
			Usage:    param.Description,
			Required: param.Required,
		}
	case "array":
		return &cli.StringSliceFlag{
			Name:     name,
			Usage:    param.Description,
			Required: param.Required,
		}
	}
	panic("Unknown param type: " + param.Type)
}
