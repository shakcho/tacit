package identityprobe

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Evidence string

const (
	EvidenceFixture       Evidence = "fixture"
	EvidenceSandbox       Evidence = "sandbox"
	EvidenceDocumentation Evidence = "documentation"
)

type Case struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Evidence    []Evidence `json:"evidence"`
}

//go:embed cases.json
var catalogJSON []byte

func Catalog() ([]Case, error) {
	var cases []Case
	decoder := json.NewDecoder(strings.NewReader(string(catalogJSON)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cases); err != nil {
		return nil, fmt.Errorf("decode identity case catalog: %w", err)
	}
	if len(cases) == 0 {
		return nil, errors.New("identity case catalog is empty")
	}

	seen := make(map[string]struct{}, len(cases))
	for index, testCase := range cases {
		if testCase.ID == "" || strings.TrimSpace(testCase.ID) != testCase.ID {
			return nil, fmt.Errorf("identity case %d has an invalid ID", index)
		}
		if _, exists := seen[testCase.ID]; exists {
			return nil, fmt.Errorf("duplicate identity case %q", testCase.ID)
		}
		seen[testCase.ID] = struct{}{}
		if strings.TrimSpace(testCase.Description) == "" || len(testCase.Evidence) == 0 {
			return nil, fmt.Errorf("identity case %q is incomplete", testCase.ID)
		}
		for _, evidence := range testCase.Evidence {
			switch evidence {
			case EvidenceFixture, EvidenceSandbox, EvidenceDocumentation:
			default:
				return nil, fmt.Errorf("identity case %q has unknown evidence %q", testCase.ID, evidence)
			}
		}
	}

	return cases, nil
}
