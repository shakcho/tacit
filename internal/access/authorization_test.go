package access

import (
	"testing"
	"time"
)

func TestCapabilityCatalogIsUniqueAndSerializable(t *testing.T) {
	t.Parallel()
	seen := make(map[Capability]struct{})
	for _, capability := range CapabilityCatalog() {
		if capability.Action.String() == "" || capability.Resource.String() == "" || capability.Scope.String() == "" {
			t.Fatalf("invalid capability: %#v", capability)
		}
		if _, exists := seen[capability]; exists {
			t.Fatalf("duplicate capability: %#v", capability)
		}
		seen[capability] = struct{}{}
	}
	if len(seen) == 0 {
		t.Fatal("capability catalog is empty")
	}
}

func TestEveryCapabilityHasPositiveNegativeCrossTenantAndRevokedCoverage(t *testing.T) {
	t.Parallel()
	organization := authorizationOrganization(t, 2)
	otherOrganization := authorizationOrganization(t, 3)
	admin := authorizationPrincipal(t, organization, 4, RoleAdmin)
	viewer := authorizationPrincipal(t, organization, 5, RoleViewer)
	expired := admin
	expired.identity.ExpiresAt = time.Now().Add(-time.Minute)

	for _, definition := range capabilityDefinitions {
		definition := definition
		t.Run(definition.capability.Action.String()+"/"+definition.capability.Resource.String()+"/"+definition.capability.Scope.String(), func(t *testing.T) {
			facts := policyFacts{
				organization: organization, resource: definition.capability.Resource,
				scope: definition.capability.Scope, id: "fixture_1", valid: true,
			}
			positive := policyAuthority{kind: authorityMember, member: admin, now: time.Now()}
			if definition.capability.Action == ActionSubmitWorkerResult {
				assignment, _ := NewWorkerAssignment(organization, definition.capability.Resource, facts.id, "job_fixture", 1, 1, 1)
				result, _ := NewWorkerResultScope(organization, definition.capability.Resource, facts.id, "job_fixture", 1, 1, 1)
				positive = policyAuthority{kind: authorityWorker, worker: assignment, result: result}
			}
			allowed := evaluateDecision(definition, positive, facts).Allowed()
			denyOnly := definition.capability.Action == ActionRead && definition.capability.Resource == ResourceAdministration
			if allowed == denyOnly {
				t.Fatalf("positive=%v, denyOnly=%v", allowed, denyOnly)
			}

			negative := evaluateDecision(
				definition,
				policyAuthority{kind: authorityMember, member: viewer, now: time.Now()},
				facts,
			)
			if negative.Allowed() && definition.capability.Scope == ScopeResource {
				t.Fatal("private resource allowed for unrelated viewer")
			}

			crossTenantFacts := facts
			crossTenantFacts.organization = otherOrganization
			if evaluateDecision(definition, positive, crossTenantFacts).Allowed() {
				t.Fatal("cross-tenant capability allowed")
			}
			if evaluateDecision(
				definition,
				policyAuthority{kind: authorityMember, member: expired, now: time.Now()},
				facts,
			).Allowed() {
				t.Fatal("revoked capability allowed")
			}
		})
	}
}

func TestContextDecisionsUseRolesAsASet(t *testing.T) {
	t.Parallel()
	organization := authorizationOrganization(t, 1)
	admin := authorizationPrincipal(t, organization, 1, RoleViewer, RoleAdmin)
	editor := authorizationPrincipal(t, organization, 2, RoleEditor)
	viewer := authorizationPrincipal(t, organization, 3, RoleViewer)

	organizationSubject := SubjectKey{value: "context/" + organization.String() + "/organization"}
	administrationSubject := SubjectKey{value: "context/" + organization.String() + "/administration"}
	meetingSubject := SubjectKey{value: "context/" + organization.String() + "/meeting"}

	if !ContextDecisions(admin).Allows(ActionRead, organizationSubject) ||
		!ContextDecisions(admin).Allows(ActionManageMembers, administrationSubject) ||
		!ContextDecisions(admin).Allows(ActionCreate, meetingSubject) {
		t.Fatal("admin context decisions are incomplete")
	}
	if ContextDecisions(editor).Allows(ActionManageMembers, administrationSubject) ||
		!ContextDecisions(editor).Allows(ActionCreate, meetingSubject) {
		t.Fatal("editor context decisions are incorrect")
	}
	if ContextDecisions(viewer).Allows(ActionCreate, meetingSubject) ||
		ContextDecisions(viewer).Allows(ActionRead, administrationSubject) {
		t.Fatal("viewer received a privileged context decision")
	}
}

func TestMemberResourcePolicyCoversRelationshipsPublicationAndTenant(t *testing.T) {
	t.Parallel()
	organization := authorizationOrganization(t, 10)
	otherOrganization := authorizationOrganization(t, 11)
	admin := authorizationPrincipal(t, organization, 10, RoleAdmin)
	editor := authorizationPrincipal(t, organization, 11, RoleEditor)
	viewer := authorizationPrincipal(t, organization, 12, RoleViewer)

	owner, _ := NewRelationships(RelationshipOwner)
	editingCollaborator, _ := NewRelationships(RelationshipEditingCollaborator)
	projectMember, _ := NewRelationships(RelationshipProjectMember)
	privateOwner := authorizationFacts(t, organization, ResourceMeeting, "mtg_owner", owner, false, false)
	privateCollaborator := authorizationFacts(t, organization, ResourceMeeting, "mtg_collaborator", editingCollaborator, false, false)
	privateProject := authorizationFacts(t, organization, ResourceMeeting, "mtg_project", projectMember, false, false)
	published := authorizationFacts(t, organization, ResourceMeeting, "mtg_published", Relationships{}, true, false)
	crossTenant := authorizationFacts(t, otherOrganization, ResourceMeeting, "mtg_other", owner, true, false)

	if !allows(ResourceDecisions(admin, privateProject), ActionUpdate) {
		t.Fatal("same-tenant admin cannot update content")
	}
	if !allows(ResourceDecisions(editor, privateOwner), ActionDelete) ||
		!allows(ResourceDecisions(editor, privateCollaborator), ActionUpdate) ||
		allows(ResourceDecisions(editor, privateCollaborator), ActionDelete) {
		t.Fatal("editor owner/collaborator decisions are incorrect")
	}
	if !allows(ResourceDecisions(viewer, privateProject), ActionRead) ||
		allows(ResourceDecisions(viewer, privateProject), ActionUpdate) ||
		!allows(ResourceDecisions(viewer, published), ActionRead) {
		t.Fatal("viewer read-only decisions are incorrect")
	}
	for _, decision := range ResourceDecisions(admin, crossTenant).Decisions() {
		if decision.Allowed() || decision.Reason() != ReasonCrossTenant {
			t.Fatalf("cross-tenant decision = %#v", decision)
		}
	}
}

func TestSystemProjectCannotBeUpdatedOrDeleted(t *testing.T) {
	t.Parallel()
	organization := authorizationOrganization(t, 20)
	admin := authorizationPrincipal(t, organization, 20, RoleAdmin)
	owner, _ := NewRelationships(RelationshipOwner, RelationshipProjectEditor)
	system := authorizationFacts(t, organization, ResourceProject, "prj_general", owner, false, true)
	decisions := ResourceDecisions(admin, system)
	for _, action := range []Action{ActionUpdate, ActionDelete} {
		decision, found := findDecision(decisions, action)
		if !found || decision.Allowed() || decision.Reason() != ReasonSystemResource {
			t.Fatalf("system project %s decision = %#v, found=%v", action.String(), decision, found)
		}
	}
}

func TestShareAuthorityIsExactReadOnlyAndRevocable(t *testing.T) {
	t.Parallel()
	organization := authorizationOrganization(t, 30)
	facts := authorizationFacts(t, organization, ResourceHighlight, "hlt_shared", Relationships{}, false, false)
	other := authorizationFacts(t, organization, ResourceHighlight, "hlt_other", Relationships{}, false, false)
	now := time.Unix(100, 0)
	share, err := NewVerifiedShare(organization, ResourceHighlight, facts.ID(), true, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	decisions := ShareDecisions(share, facts, now)
	if !allows(decisions, ActionRead) || !allows(decisions, ActionIssueMediaGrant) {
		t.Fatal("active share cannot read its exact scope")
	}
	for _, action := range []Action{ActionUpdate, ActionDelete, ActionSearch, ActionSubscribe} {
		if allows(decisions, action) {
			t.Fatalf("share unexpectedly allows %s", action.String())
		}
	}
	if allows(ShareDecisions(share, other, now), ActionRead) ||
		allows(ShareDecisions(share, facts, now.Add(2*time.Minute)), ActionRead) {
		t.Fatal("share escaped its resource or expiry scope")
	}
	withoutMedia, _ := NewVerifiedShare(organization, ResourceHighlight, facts.ID(), false, now.Add(time.Minute))
	if allows(ShareDecisions(withoutMedia, facts, now), ActionIssueMediaGrant) {
		t.Fatal("read-only share issued media access")
	}
}

func TestWorkerAuthorityIsBoundToExactAssignmentScope(t *testing.T) {
	t.Parallel()
	organization := authorizationOrganization(t, 40)
	facts := authorizationFacts(t, organization, ResourceTranscript, "trn_1", Relationships{}, false, false)
	other := authorizationFacts(t, organization, ResourceTranscript, "trn_2", Relationships{}, false, false)
	assignment, err := NewWorkerAssignment(organization, ResourceTranscript, facts.ID(), "job_1", 1, 9, 3)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewWorkerResultScope(organization, ResourceTranscript, facts.ID(), "job_1", 1, 9, 3)
	if err != nil {
		t.Fatal(err)
	}
	stale, _ := NewWorkerResultScope(organization, ResourceTranscript, facts.ID(), "job_1", 1, 8, 3)
	decisions := WorkerDecisions(assignment, result, facts)
	if !allows(decisions, ActionSubmitWorkerResult) || allows(decisions, ActionUpdate) ||
		allows(WorkerDecisions(assignment, result, other), ActionSubmitWorkerResult) ||
		allows(WorkerDecisions(assignment, stale, facts), ActionSubmitWorkerResult) {
		t.Fatal("worker assignment scope is not exact")
	}
}

func TestExpiredAndZeroAuthoritiesFailClosed(t *testing.T) {
	t.Parallel()
	organization := authorizationOrganization(t, 50)
	facts := authorizationFacts(t, organization, ResourceMeeting, "mtg_1", Relationships{}, true, false)
	expired := authorizationPrincipal(t, organization, 50, RoleAdmin)
	expired.identity.ExpiresAt = time.Now().Add(-time.Minute)
	for _, principal := range []LocalPrincipal{{}, expired} {
		for _, decision := range ResourceDecisions(principal, facts).Decisions() {
			if decision.Allowed() || decision.Reason() != ReasonInvalidAuthority {
				t.Fatalf("invalid principal decision = %#v", decision)
			}
		}
	}
}

func authorizationOrganization(t *testing.T, value byte) OrganizationID {
	t.Helper()
	bytes := [16]byte{15: value}
	return OrganizationID{value: bytes}
}

func authorizationPrincipal(t *testing.T, organization OrganizationID, user byte, roleValues ...Role) LocalPrincipal {
	t.Helper()
	roles, err := NewRoles(roleValues...)
	if err != nil {
		t.Fatal(err)
	}
	namespace, _ := ParseIdentityNamespace("workos-test")
	subject, _ := ParseExternalSubject("user-test")
	externalOrganization, _ := ParseExternalOrganizationID("organization-test")
	session, _ := ParseExternalSessionID("session-test")
	return LocalPrincipal{
		identity: VerifiedIdentity{
			Namespace: namespace, Subject: subject, Organization: externalOrganization, Session: session,
			IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
		},
		admission: LocalAdmission{User: UserID{value: [16]byte{15: user}}, Organization: organization, Roles: roles},
		valid:     true,
	}
}

func authorizationFacts(
	t *testing.T,
	organization OrganizationID,
	resource Resource,
	id string,
	relationships Relationships,
	published bool,
	system bool,
) ResourceFacts {
	t.Helper()
	facts, err := NewResourceFacts(organization, resource, id, relationships, published, system)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

func allows(decisions DecisionSet, action Action) bool {
	for _, decision := range decisions.Decisions() {
		if decision.Action() == action {
			return decision.Allowed()
		}
	}
	return false
}

func findDecision(decisions DecisionSet, action Action) (Decision, bool) {
	for _, decision := range decisions.Decisions() {
		if decision.Action() == action {
			return decision, true
		}
	}
	return Decision{}, false
}
