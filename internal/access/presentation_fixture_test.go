package access

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

type presentationFixture struct {
	Snapshots []presentationSnapshot `json:"snapshots"`
}

type presentationSnapshot struct {
	Name           string                  `json:"name"`
	PolicyRevision string                  `json:"policyRevision"`
	Decisions      []presentationDecision  `json:"decisions"`
	Assertions     []presentationAssertion `json:"assertions"`
}

type presentationDecision struct {
	Action   string `json:"action"`
	Allowed  bool   `json:"allowed"`
	Resource string `json:"resource"`
	Scope    string `json:"scope"`
	Subject  string `json:"subject"`
}

type presentationAssertion struct {
	Action  string `json:"action"`
	Allowed bool   `json:"allowed"`
	Subject string `json:"subject"`
}

func TestPresentationFixtureMatchesGoDecisions(t *testing.T) {
	t.Parallel()
	organization := authorizationOrganization(t, 60)
	admin := authorizationPrincipal(t, organization, 60, RoleAdmin)
	viewer := authorizationPrincipal(t, organization, 61, RoleViewer)
	published := authorizationFacts(t, organization, ResourceMeeting, "mtg_fixture", Relationships{}, true, false)

	fixture := presentationFixture{Snapshots: []presentationSnapshot{
		presentationFixtureSnapshot(
			"admin-context",
			ContextDecisions(admin),
			[]presentationAssertion{
				{Action: "manage_members", Allowed: true, Subject: "context/" + organization.String() + "/administration"},
				{Action: "create", Allowed: true, Subject: "context/" + organization.String() + "/meeting"},
			},
		),
		presentationFixtureSnapshot(
			"viewer-published-meeting",
			ResourceDecisions(viewer, published),
			[]presentationAssertion{
				{Action: "read", Allowed: true, Subject: published.subject().String()},
				{Action: "update", Allowed: false, Subject: published.subject().String()},
			},
		),
	}}
	actual, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')
	expected, err := os.ReadFile("testdata/presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("presentation fixture drifted; replace testdata/presentation.json with:\n%s", actual)
	}
}

func presentationFixtureSnapshot(
	name string,
	set DecisionSet,
	assertions []presentationAssertion,
) presentationSnapshot {
	decisions := set.Decisions()
	values := make([]presentationDecision, 0, len(decisions))
	for _, decision := range decisions {
		values = append(values, presentationDecision{
			Action: decision.Action().String(), Allowed: decision.Allowed(),
			Resource: decision.Resource().String(), Scope: decision.Scope().String(),
			Subject: decision.Subject().String(),
		})
	}
	return presentationSnapshot{
		Name: name, PolicyRevision: PolicyRevision, Decisions: values, Assertions: assertions,
	}
}
