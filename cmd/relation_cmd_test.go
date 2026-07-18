package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func resetRelationAddFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"blocks", "blocked-by", "related", "duplicate", "similar"} {
		flag := issueRelationAddCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("missing flag %q", name)
		}
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", name, err)
		}
		flag.Changed = false
	}
}

// mockIssueResponse returns a mock GraphQL response for an issue query.
func mockIssueResponse(id, identifier, title string) string {
	return `{"data":{"issue":{"id":"` + id + `","identifier":"` + identifier + `","title":"` + title + `","description":"","priority":0,"createdAt":"2024-01-01T00:00:00Z","updatedAt":"2024-01-01T00:00:00Z","url":"","branchName":"","number":0,"boardOrder":0,"subIssueSortOrder":0,"priorityLabel":"","reactions":[],"slackIssueComments":[],"customerTickets":[],"previousIdentifiers":[]}}}`
}

func TestRelationAddBlocksSendsCorrectMutation(t *testing.T) {
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()
	t.Setenv("LINCTL_API_KEY", "test-key")
	viper.Set("plaintext", true)
	viper.Set("json", false)
	resetRelationAddFlags(t)

	var capturedInput map[string]interface{}

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var gqlReq gqlCommandTestRequest
		if err := json.NewDecoder(req.Body).Decode(&gqlReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		switch {
		case strings.Contains(gqlReq.Query, "query Issue("):
			// Determine which issue is being fetched by inspecting the variable
			issueIDVar, _ := gqlReq.Variables["id"].(string)
			if issueIDVar == "LIN-100" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(mockIssueResponse("uuid-100", "LIN-100", "Parent issue"))),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(mockIssueResponse("uuid-200", "LIN-200", "Child issue"))),
			}, nil

		case strings.Contains(gqlReq.Query, "mutation IssueRelationCreate("):
			input, ok := gqlReq.Variables["input"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected input map, got %#v", gqlReq.Variables["input"])
			}
			capturedInput = input
			body := `{"data":{"issueRelationCreate":{"success":true,"issueRelation":{"id":"rel-1","type":"blocks","issue":{"id":"uuid-200","identifier":"LIN-200","title":"Child issue"},"relatedIssue":{"id":"uuid-100","identifier":"LIN-100","title":"Parent issue"}}}}}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}

		t.Fatalf("unexpected query: %s", gqlReq.Query)
		return nil, nil
	})

	// "LIN-100 --blocks LIN-200" means LIN-100 blocks LIN-200.
	issueRelationAddCmd.Flags().Set("blocks", "LIN-200")
	issueRelationAddCmd.Run(issueRelationAddCmd, []string{"LIN-100"})

	// Linear's verified semantics (SOL-207, confirmed empirically 2026-07-10):
	// issueRelationCreate(issueId: A, relatedIssueId: B, type: blocks) => A blocks B.
	// These assertions are written in terms of blocker/blocked so they encode
	// the API's meaning, not whatever payload the implementation happens to send.
	blocker, blocked := capturedInput["issueId"], capturedInput["relatedIssueId"]
	if blocker != "uuid-100" {
		t.Fatalf("blocker must be LIN-100 (uuid-100), got issueId=%v", blocker)
	}
	if blocked != "uuid-200" {
		t.Fatalf("blocked must be LIN-200 (uuid-200), got relatedIssueId=%v", blocked)
	}
	if capturedInput["type"] != "blocks" {
		t.Fatalf("expected type=blocks, got %v", capturedInput["type"])
	}
}

func TestRelationAddBlockedBySendsCorrectMutation(t *testing.T) {
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()
	t.Setenv("LINCTL_API_KEY", "test-key")
	viper.Set("plaintext", true)
	viper.Set("json", false)
	resetRelationAddFlags(t)

	var capturedInput map[string]interface{}

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var gqlReq gqlCommandTestRequest
		if err := json.NewDecoder(req.Body).Decode(&gqlReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		switch {
		case strings.Contains(gqlReq.Query, "query Issue("):
			issueIDVar, _ := gqlReq.Variables["id"].(string)
			if issueIDVar == "LIN-100" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(mockIssueResponse("uuid-100", "LIN-100", "Blocked issue"))),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(mockIssueResponse("uuid-200", "LIN-200", "Blocker issue"))),
			}, nil

		case strings.Contains(gqlReq.Query, "mutation IssueRelationCreate("):
			input, ok := gqlReq.Variables["input"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected input map, got %#v", gqlReq.Variables["input"])
			}
			capturedInput = input
			body := `{"data":{"issueRelationCreate":{"success":true,"issueRelation":{"id":"rel-2","type":"blocks","issue":{"id":"uuid-100","identifier":"LIN-100","title":"Blocked issue"},"relatedIssue":{"id":"uuid-200","identifier":"LIN-200","title":"Blocker issue"}}}}}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}

		t.Fatalf("unexpected query: %s", gqlReq.Query)
		return nil, nil
	})

	// "LIN-100 --blocked-by LIN-200" means LIN-200 blocks LIN-100.
	issueRelationAddCmd.Flags().Set("blocked-by", "LIN-200")
	issueRelationAddCmd.Run(issueRelationAddCmd, []string{"LIN-100"})

	// Linear's verified semantics (SOL-207): issueId blocks relatedIssueId.
	// The blocker here is LIN-200 — asserting the payload the old code sent
	// (issueId=uuid-100) would re-certify the inversion.
	blocker, blocked := capturedInput["issueId"], capturedInput["relatedIssueId"]
	if blocker != "uuid-200" {
		t.Fatalf("blocker must be LIN-200 (uuid-200), got issueId=%v", blocker)
	}
	if blocked != "uuid-100" {
		t.Fatalf("blocked must be LIN-100 (uuid-100), got relatedIssueId=%v", blocked)
	}
	if capturedInput["type"] != "blocks" {
		t.Fatalf("expected type=blocks, got %v", capturedInput["type"])
	}
}

func TestRelationListShowsRelations(t *testing.T) {
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()
	t.Setenv("LINCTL_API_KEY", "test-key")
	viper.Set("plaintext", true)
	viper.Set("json", false)

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var gqlReq gqlCommandTestRequest
		if err := json.NewDecoder(req.Body).Decode(&gqlReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if !strings.Contains(gqlReq.Query, "IssueRelations") {
			t.Fatalf("expected IssueRelations query, got: %s", gqlReq.Query)
		}

		// In Linear's schema, `relations` holds edges where LIN-100 is the
		// issueId (LIN-100 blocks relatedIssue), and `inverseRelations` holds
		// edges where LIN-100 is the relatedIssue (issue blocks LIN-100).
		// Forward: LIN-100 blocks LIN-200. Inverse: LIN-300 blocks LIN-100.
		body := `{"data":{"issue":{
			"relations":{"nodes":[
				{"id":"rel-1","type":"blocks","issue":{"id":"uuid-100","identifier":"LIN-100","title":"This issue"},"relatedIssue":{"id":"uuid-200","identifier":"LIN-200","title":"Downstream task"}}
			]},
			"inverseRelations":{"nodes":[
				{"id":"rel-2","type":"blocks","issue":{"id":"uuid-300","identifier":"LIN-300","title":"Upstream blocker"},"relatedIssue":{"id":"uuid-100","identifier":"LIN-100","title":"This issue"}}
			]}
		}}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	// Capture stdout to verify direction-aware labels
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	issueRelationListCmd.Run(issueRelationListCmd, []string{"LIN-100"})

	w.Close()
	var buf strings.Builder
	io.Copy(&buf, r)
	os.Stdout = oldStdout
	got := buf.String()

	lines := strings.Split(got, "\n")
	var forwardLine, inverseLine string
	for _, line := range lines {
		if strings.Contains(line, "rel-1") {
			forwardLine = line
		}
		if strings.Contains(line, "rel-2") {
			inverseLine = line
		}
	}

	// Forward relation: LIN-100 blocks LIN-200 → "blocks LIN-200"
	if !strings.Contains(forwardLine, "blocks") || strings.Contains(forwardLine, "blocked by") {
		t.Fatalf("forward relation must be labeled 'blocks', got:\n%s", got)
	}
	if !strings.Contains(forwardLine, "LIN-200") {
		t.Fatalf("forward relation must show the other issue LIN-200, got:\n%s", got)
	}

	// Inverse relation: LIN-300 blocks LIN-100 → "blocked by LIN-300"
	if !strings.Contains(inverseLine, "blocked by") {
		t.Fatalf("inverse relation must be labeled 'blocked by', got:\n%s", got)
	}
	if !strings.Contains(inverseLine, "LIN-300") {
		t.Fatalf("inverse relation must show the other issue LIN-300, got:\n%s", got)
	}

	// An issue must never be listed as its own related issue (SOL-207
	// secondary defect: the inverse edge's relatedIssue IS the queried issue).
	for _, line := range []string{forwardLine, inverseLine} {
		if strings.Contains(line, "LIN-100") {
			t.Fatalf("LIN-100 listed as its own related issue:\n%s", got)
		}
	}
}

func TestRelationRemoveSendsDeleteMutation(t *testing.T) {
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()
	t.Setenv("LINCTL_API_KEY", "test-key")
	viper.Set("plaintext", true)
	viper.Set("json", false)

	var capturedID string

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var gqlReq gqlCommandTestRequest
		if err := json.NewDecoder(req.Body).Decode(&gqlReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if !strings.Contains(gqlReq.Query, "mutation IssueRelationDelete(") {
			t.Fatalf("expected IssueRelationDelete mutation, got: %s", gqlReq.Query)
		}

		capturedID, _ = gqlReq.Variables["id"].(string)

		body := `{"data":{"issueRelationDelete":{"success":true}}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	issueRelationRemoveCmd.Run(issueRelationRemoveCmd, []string{"rel-abc-123"})

	if capturedID != "rel-abc-123" {
		t.Fatalf("expected relation ID rel-abc-123, got %v", capturedID)
	}
}

func TestRelationTypeLabel(t *testing.T) {
	// Forward (non-inverse): the queried issue is the edge's issueId, so
	// type=blocks reads "this issue blocks the other" (SOL-207).
	forwardCases := map[string]string{
		"blocks":    "blocks",
		"duplicate": "duplicate of",
		"related":   "related to",
		"similar":   "similar to",
		"unknown":   "unknown",
	}
	for input, want := range forwardCases {
		if got := relationTypeLabel(input, false); got != want {
			t.Fatalf("relationTypeLabel(%q, false) = %q, want %q", input, got, want)
		}
	}

	// Inverse: the queried issue is the edge's relatedIssueId — flipped.
	inverseCases := map[string]string{
		"blocks":    "blocked by",
		"duplicate": "has duplicate",
		"related":   "related to",
		"similar":   "similar to",
	}
	for input, want := range inverseCases {
		if got := relationTypeLabel(input, true); got != want {
			t.Fatalf("relationTypeLabel(%q, true) = %q, want %q", input, got, want)
		}
	}
}
