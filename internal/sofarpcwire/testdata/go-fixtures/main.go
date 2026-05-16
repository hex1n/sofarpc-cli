package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
)

type goldenWireFixture struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	ContentHex string            `json:"contentHex"`
	Want       goldenFixtureWant `json:"want"`
}

type goldenFixtureWant struct {
	Service                 string          `json:"service,omitempty"`
	Method                  string          `json:"method,omitempty"`
	ParamTypes              []string        `json:"paramTypes,omitempty"`
	TargetServiceUniqueName string          `json:"targetServiceUniqueName,omitempty"`
	ArgsJSON                json.RawMessage `json:"argsJson,omitempty"`
}

func main() {
	outputDir := defaultOutputDir()
	if len(os.Args) > 1 {
		outputDir = os.Args[1]
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		die(err)
	}

	fixture, err := requestContentFixture()
	if err != nil {
		die(err)
	}

	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		die(err)
	}
	data = append(data, '\n')

	path := filepath.Join(outputDir, "request-query-fixture.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		die(err)
	}
	fmt.Println(path)
}

func requestContentFixture() (goldenWireFixture, error) {
	const argsJSON = `[
  {
    "type": "com.example.FixtureRequest",
    "fields": {
      "amount": {
        "type": "java.math.BigDecimal",
        "value": "1000.50"
      },
      "id": 1001,
      "items": {
        "type": "java.util.ArrayList",
        "items": [
          {
            "type": "com.example.FixtureItem",
            "fields": {
              "amount": null,
              "code": "primary",
              "status": "ACTIVE"
            }
          }
        ]
      },
      "status": "ACTIVE"
    }
  }
]`

	req, err := sofarpcwire.BuildGenericRequest(sofarpcwire.RequestSpec{
		Service: "com.example.FixtureFacade",
		Method:  "query",
		ParamTypes: []string{
			"com.example.FixtureRequest",
		},
		Args: []any{
			map[string]any{
				"@type": "com.example.FixtureRequest",
				"id":    int64(1001),
				"amount": map[string]any{
					"@type": "java.math.BigDecimal",
					"value": "1000.50",
				},
				"status": "ACTIVE",
				"items": []any{
					map[string]any{
						"@type":  "com.example.FixtureItem",
						"code":   "primary",
						"status": "ACTIVE",
					},
				},
			},
		},
	})
	if err != nil {
		return goldenWireFixture{}, err
	}

	return goldenWireFixture{
		Name:       "Go-encoded generic query request with DTO, BigDecimal and list",
		Kind:       "request-content",
		ContentHex: hex.EncodeToString(req.Content),
		Want: goldenFixtureWant{
			Service:                 "com.example.FixtureFacade",
			Method:                  "query",
			ParamTypes:              []string{"com.example.FixtureRequest"},
			TargetServiceUniqueName: "com.example.FixtureFacade:1.0",
			ArgsJSON:                json.RawMessage(argsJSON),
		},
	}, nil
}

func defaultOutputDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("internal", "sofarpcwire", "testdata", "golden")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "golden"))
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
