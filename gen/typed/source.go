package typed

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// readSource handles file://, http://, https://, and bare-path inputs.
// It mirrors spec.OpenApiSpec.ReadSource but is local to the typed
// generator so the gen package has no runtime dep on okapi/spec.
func readSource(source string) ([]byte, error) {
	if strings.HasPrefix(source, "file://") {
		return os.ReadFile(strings.TrimPrefix(source, "file://"))
	}
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		resp, err := http.Get(source)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("GET %s: status %d", source, resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	}
	// Treat as a local file path.
	return os.ReadFile(source)
}
