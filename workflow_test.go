package main

import (
	"slices"
	"strings"
	"testing"
)

func TestParseWorkflow(t *testing.T) {
	name, uses, err := parseWorkflow([]byte(sampleWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	if name != "CI" {
		t.Errorf("name = %q, want CI", name)
	}
	want := []string{"actions/checkout@v3.5.0", "actions/setup-python@v4.0.0"}
	if !slices.Equal(uses, want) {
		t.Errorf("uses = %v, want %v", uses, want)
	}
}

func TestParseWorkflowKeepsFileOrderWithoutDuplicates(t *testing.T) {
	content := `jobs:
  zeta:
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/local
      - uses: docker://alpine:3.20
  alpha:
    uses: owner/repo/.github/workflows/reusable.yml@v1
  beta:
    steps:
      - uses: actions/checkout@v4
      - uses: "actions/setup-go@v5"
      - run: echo hi
`
	name, uses, err := parseWorkflow([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
	want := []string{"actions/checkout@v4", "./.github/actions/local", "docker://alpine:3.20", "actions/setup-go@v5"}
	if !slices.Equal(uses, want) {
		t.Errorf("uses = %v, want %v", uses, want)
	}
}

func TestParseWorkflowUnicode(t *testing.T) {
	name, _, err := parseWorkflow([]byte("name: Ünïcödé CI\njobs: {}\n"))
	if err != nil || name != "Ünïcödé CI" {
		t.Errorf("got %q, %v", name, err)
	}
}

func TestParseWorkflowInvalid(t *testing.T) {
	for _, content := range []string{"jobs: [", "jobs:\n  build:\n    steps: not-a-list\n"} {
		if _, _, err := parseWorkflow([]byte(content)); err == nil {
			t.Errorf("parseWorkflow(%q) did not fail", content)
		}
	}
}

func TestIsVersionedAction(t *testing.T) {
	tests := map[string]bool{
		"actions/checkout@v4":                     true,
		"actions/upload-artifact/merge@v4":        true,
		"actions/checkout":                        false,
		"./.github/actions/local":                 false,
		"docker://alpine@sha256:0123456789abcdef": false,
	}
	for uses, want := range tests {
		if got := isVersionedAction(uses); got != want {
			t.Errorf("isVersionedAction(%q) = %v, want %v", uses, got, want)
		}
	}
}

func TestApplyUpdates(t *testing.T) {
	updates := []actionVersion{
		{Name: "actions/checkout", Current: "v3.5.0", Latest: "v4.0.0"},
		{Name: "actions/setup-python", Current: "v4.0.0", Latest: "v5.0.0"},
	}
	got := applyUpdates(sampleWorkflow, updates)
	want := strings.NewReplacer("actions/checkout@v3.5.0", "actions/checkout@v4.0.0",
		"actions/setup-python@v4.0.0", "actions/setup-python@v5.0.0").Replace(sampleWorkflow)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestApplyUpdatesSkipsActionsWithoutLatest(t *testing.T) {
	got := applyUpdates(sampleWorkflow, []actionVersion{{Name: "actions/checkout", Current: "v3.5.0"}})
	if got != sampleWorkflow {
		t.Errorf("content changed:\n%s", got)
	}
}

func TestApplyUpdatesOnlyReplacesWholeReferences(t *testing.T) {
	content := `steps:
  - uses: actions/checkout@v4
  - uses: actions/checkout@v4.1.0
  - uses: myorg/actions/checkout@v4
  - uses: 'actions/checkout@v4'
  - uses: "actions/checkout@v4" # pinned
  - {uses: actions/checkout@v4, with: {fetch-depth: 0}}
  - uses: actions/checkout@v4
`
	want := `steps:
  - uses: actions/checkout@v5.0.0
  - uses: actions/checkout@v4.1.0
  - uses: myorg/actions/checkout@v4
  - uses: 'actions/checkout@v5.0.0'
  - uses: "actions/checkout@v5.0.0" # pinned
  - {uses: actions/checkout@v5.0.0, with: {fetch-depth: 0}}
  - uses: actions/checkout@v5.0.0
`
	got := applyUpdates(content, []actionVersion{{Name: "actions/checkout", Current: "v4", Latest: "v5.0.0"}})
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestApplyUpdatesSHAPin(t *testing.T) {
	content := "      - uses: actions/checkout@" + shaA + " # v4.1.0\n"
	got := applyUpdates(content, []actionVersion{{Name: "actions/checkout", Current: shaA, Latest: "v5"}})
	if want := "      - uses: actions/checkout@v5 # v4.1.0\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
