package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi/internal/log"
	"github.com/jathanism/okapi/request"
)

// Debug enables or disables debug logging. If the DEBUG environment variable
// is set, debug logging is enabled. Otherwise, it is disabled.
func Debug() {
	enabled := os.Getenv("DEBUG") != ""
	if enabled {
		log.DetectLevel()
		if log.GetLevel() == log.TraceLevel {
			log.Trace("Trace output enabled")
		} else {
			log.Debug("Debug output enabled")
		}
	} else {
		// This is a fake level that should suppress all logging
		log.SetLevel(log.NoLogsLevel)
	}
}

// MockRequestJson returns a function which can be used with openapi.OpenApi to
// mock a request handler and inspect the response.
//
// Example:
//
//	inspect, requestJson := MockRequestJson()
//	openApiInstance.With(requestJson)
//	openApiInstance.SomeEndpoint()
//	method, uri, body := inspect()
func MockRequestJson() (func() (string, string, string), request.RequestOption) {
	var reqMethod string
	var reqUri string
	var bodyValue []byte

	inspect := func() (string, string, string) {
		return reqMethod, reqUri, string(bodyValue)
	}

	requestJson := func(method string, uri string, body io.Reader, result any, headers map[string][]string) (*http.Response, error) {
		var err error
		reqMethod = method
		reqUri = uri
		bodyValue, err = io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		_, err = url.Parse(uri)
		return nil, err
	}

	return inspect, request.RequestJSON(requestJson)
}

// RunTestsIfMatch allows us to declare tests that only run if a specific test suite
// is matched, by calling the package's test suite.
//
// This is a workaround because Ginkgo only allows you to declare one suite per
// package.
func RunTestsIfMatch(name string, packageSuite func(t *testing.T), t *testing.T) {
	re := regexp.MustCompile(`-test.run=\^` + name + `\$`)
	for i, arg := range os.Args {
		if re.MatchString(arg) {
			Trace("Running suite ", name)
			packageSuite(t)
		} else if arg == "-test.run" || arg == "--test.run" {
			if i < len(os.Args)-1 {
				next := os.Args[i+1]
				if next == `^`+name+`$` {
					Trace("Running split arg ", name)
					packageSuite(t)
				}
			}
		}
	}
	Trace("RunTestsIfMatch ", name, " ending")
}

// RunTestsWithFocus allows us to define a TestMain which will only run tests
// that match the given `-test.run` flag. This is to make Ginkgo Focus work with
// the Golang test suite flags and the VSCode test runner.
func RunTestsWithFocus(m *testing.M) {
	var hasArgs = false
	var testMatch = ""
	for i, arg := range os.Args {
		if strings.HasSuffix(arg, "-args") {
			hasArgs = true
		}
		if strings.HasPrefix(arg, "-test.run") {
			testMatch = arg
		}
		if arg == "-test.run" || arg == "--test.run" {
			if i < len(os.Args)-1 {
				testMatch += "=" + os.Args[i+1]
			}
		}
	}

	if !hasArgs && testMatch != "" {
		match, ok := strings.CutPrefix(testMatch, "-test.run=^Test")
		if !ok {
			Trace("Failed to parse test match: ", testMatch)
			Trace("   os.Args: ", os.Args)
		} else {
			match, ok = strings.CutSuffix(match, "$")
			if !ok {
				Trace("Failed to parse test match: ", testMatch)
			}
			Trace("-ginkgo.focus", match)
			os.Args = append(os.Args, "-ginkgo.focus", match)
		}
	}

	os.Exit(m.Run())
}

// generateRandomString generates a random string of the given length.
func generateRandomString(length int) string {
	b := make([]byte, length)
	_, err := rand.Read(b)
	Expect(err).ToNot(HaveOccurred())
	return hex.EncodeToString(b)
}

// Trace conditionally prints to stdout if DEBUG=trace is set.
func Trace(args ...any) {
	if os.Getenv("DEBUG") == "trace" {
		for _, arg := range args {
			fmt.Print(arg)
		}
	}
}
