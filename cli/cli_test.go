package cli_test

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi"
	"github.com/jathanism/okapi/cli"
	"github.com/jathanism/okapi/internal/testutil"
	"github.com/jathanism/okapi/request"
)

const testSource = "file://../testdata/openapi.yaml"

// mockCliContext implements cli.CliContext for testing.
type mockCliContext struct {
	host     string
	client   request.OpenApiClient
	lastJson any
	lastErr  string
}

func (m *mockCliContext) Stdin() *os.File   { return os.Stdin }
func (m *mockCliContext) Stdout() io.Writer { return os.Stdout }
func (m *mockCliContext) Host() string      { return m.host }
func (m *mockCliContext) SetHost(h string)  { m.host = h }

func (m *mockCliContext) GetApiClient() request.OpenApiClient {
	return m.client
}

func (m *mockCliContext) EmitJsonObj(obj any) error {
	m.lastJson = obj
	return nil
}

func (m *mockCliContext) Error(msg string) error {
	m.lastErr = msg
	return io.EOF
}

// mockApiClient implements request.OpenApiClient
type mockApiClient struct {
	responseBody string
	statusCode   int
}

func (m *mockApiClient) RequestJSON(method string, uri string, body io.Reader, result any, headers map[string][]string) (*http.Response, error) {
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(strings.NewReader(m.responseBody)),
	}, nil
}

func TestOpenApiCli(t *testing.T) {
	RegisterFailHandler(Fail)

	testutil.Debug()

	RunSpecs(t, "cli")
}

var _ = Describe("OpenApiCli", Ordered, func() {
	var api *openapi.OpenApi
	var openApiCli *cli.OpenApiCli

	BeforeAll(func() {
		api, _ = (*openapi.OpenApi)(nil).NewFromSource(testSource)
		openApiCli = (&cli.OpenApiCli{
			Api:     api,
			AppName: "okapi",
		}).New()
	})

	It("builds commands from the spec", func() {
		commands := openApiCli.Commands()
		Expect(commands).ToNot(BeEmpty())
		names := make([]string, len(commands))
		for i, c := range commands {
			names[i] = c.Name
		}
		Expect(names).To(ContainElements("accounts", "users", "organizations", "schemas"))
	})
})

var _ = Describe("OpenApiCli --host", Ordered, func() {
	testHost := "example.com"

	var api *openapi.OpenApi
	var openApiCli *cli.OpenApiCli

	BeforeAll(func() {
		api, _ = (*openapi.OpenApi)(nil).NewFromSource(testSource)
		openApiCli = (&cli.OpenApiCli{
			Api:      api,
			AppName:  "okapi",
			HostFlag: cli.DefaultHostFlag("", ""),
		}).New()
	})

	It("respects --host as a base flag", func() {
		ctx := &mockCliContext{
			host: testHost,
			client: &mockApiClient{
				responseBody: `[{"id":"test"}]`,
				statusCode:   200,
			},
		}

		err := openApiCli.Run(ctx, []string{"okapi", "--host", testHost, "users", "list"})
		Expect(err).ToNot(HaveOccurred())
		Expect(ctx.Host()).To(Equal(testHost))
	})

	It("respects --host as a middle flag", func() {
		ctx := &mockCliContext{
			host: testHost,
			client: &mockApiClient{
				responseBody: `[{"id":"test"}]`,
				statusCode:   200,
			},
		}

		err := openApiCli.Run(ctx, []string{"okapi", "users", "--host", testHost, "list"})
		Expect(err).ToNot(HaveOccurred())
		Expect(ctx.Host()).To(Equal(testHost))
	})

	It("respects --host as a trailing flag", func() {
		ctx := &mockCliContext{
			host: testHost,
			client: &mockApiClient{
				responseBody: `[{"id":"test"}]`,
				statusCode:   200,
			},
		}

		err := openApiCli.Run(ctx, []string{"okapi", "users", "list", "--host", testHost})
		Expect(err).ToNot(HaveOccurred())
		Expect(ctx.Host()).To(Equal(testHost))
	})
})

var _ = Describe("ListPrint", func() {
	var buf *strings.Builder

	BeforeEach(func() {
		buf = &strings.Builder{}
	})

	foobar := map[string]string{"--foo": "bar"}
	realCmds := map[string]string{
		"create":   "Create a new record",
		"decrypt":  "Decrypt data",
		"destroy":  "Delete a record",
		"list":     "List records",
		"retrieve": "Retrieve a record",
		"search":   "Search for records",
	}

	It("works with a single item", func() {
		cli.ListPrint(buf, "Test", foobar, 0, 1, 80)
		Expect(buf.String()).To(ContainSubstring("--foo bar"))
	})

	It("indents nicely", func() {
		cli.ListPrint(buf, "Test", foobar, 2, 1, 80)
		Expect(buf.String()).To(ContainSubstring("  --foo bar"))
	})

	It("spaces offsets nicely", func() {
		cli.ListPrint(buf, "Test", foobar, 2, 4, 80)
		Expect(buf.String()).To(ContainSubstring("  --foo    bar"))
	})

	It("can format real command lists", func() {
		cli.ListPrint(buf, "Test", realCmds, 2, 4, 60)
		result := buf.String()
		Expect(result).To(ContainSubstring("  create      Create a new record"))
		Expect(result).To(ContainSubstring("  list        List records"))
	})
})
