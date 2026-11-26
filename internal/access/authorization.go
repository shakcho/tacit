package access

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const PolicyRevision = "tacit-authorization-v1"

var (
	ErrAuthorizationResource = errors.New("invalid authorization resource")
	ErrAuthorizationScope    = errors.New("invalid authorization scope")
)

type Action uint8

const (
	ActionRead Action = iota + 1
	ActionCreate
	ActionUpdate
	ActionDelete
	ActionManageMembers
	ActionManageCollaborators
	ActionPublish
	ActionManageShares
	ActionIssueMediaGrant
	ActionSubscribe
	ActionSearch
	ActionEnqueueWork
	ActionSubmitWorkerResult
)

func (action Action) String() string {
	switch action {
	case ActionRead:
		return "read"
	case ActionCreate:
		return "create"
	case ActionUpdate:
		return "update"
	case ActionDelete:
		return "delete"
	case ActionManageMembers:
		return "manage_members"
	case ActionManageCollaborators:
		return "manage_collaborators"
	case ActionPublish:
		return "publish"
	case ActionManageShares:
		return "manage_shares"
	case ActionIssueMediaGrant:
		return "issue_media_grant"
	case ActionSubscribe:
		return "subscribe"
	case ActionSearch:
		return "search"
	case ActionEnqueueWork:
		return "enqueue_work"
	case ActionSubmitWorkerResult:
		return "submit_worker_result"
	default:
		return ""
	}
}

type Resource uint8

const (
	ResourceOrganization Resource = iota + 1
	ResourceProject
	ResourceMeeting
	ResourceRecording
	ResourceTranscript
	ResourceHighlight
	ResourcePlaylist
	ResourceShare
	ResourceAdministration
)

func (resource Resource) String() string {
	switch resource {
	case ResourceOrganization:
		return "organization"
	case ResourceProject:
		return "project"
	case ResourceMeeting:
		return "meeting"
	case ResourceRecording:
		return "recording"
	case ResourceTranscript:
		return "transcript"
	case ResourceHighlight:
		return "highlight"
	case ResourcePlaylist:
		return "playlist"
	case ResourceShare:
		return "share"
	case ResourceAdministration:
		return "administration"
	default:
		return ""
	}
}

type AuthorizationScope uint8

const (
	ScopeContext AuthorizationScope = iota + 1
	ScopeResource
)

func (scope AuthorizationScope) String() string {
	switch scope {
	case ScopeContext:
		return "context"
	case ScopeResource:
		return "resource"
	default:
		return ""
	}
}

type Capability struct {
	Action   Action
	Resource Resource
	Scope    AuthorizationScope
}

type DecisionReason uint8

const (
	ReasonDenied DecisionReason = iota
	ReasonAllowed
	ReasonInvalidAuthority
	ReasonCrossTenant
	ReasonSystemResource
	ReasonUnsupportedCapability
)

type SubjectKey struct{ value string }

func (key SubjectKey) String() string { return key.value }

type Decision struct {
	capability Capability
	subject    SubjectKey
	allowed    bool
	reason     DecisionReason
}

func (decision Decision) Action() Action            { return decision.capability.Action }
func (decision Decision) Resource() Resource        { return decision.capability.Resource }
func (decision Decision) Scope() AuthorizationScope { return decision.capability.Scope }
func (decision Decision) Subject() SubjectKey       { return decision.subject }
func (decision Decision) Allowed() bool             { return decision.allowed }
func (decision Decision) Reason() DecisionReason    { return decision.reason }

type DecisionSet struct{ decisions []Decision }

func (set DecisionSet) Decisions() []Decision { return slices.Clone(set.decisions) }

func (set DecisionSet) Allows(action Action, subject SubjectKey) bool {
	for _, decision := range set.decisions {
		if decision.allowed && decision.capability.Action == action && decision.subject == subject {
			return true
		}
	}
	return false
}

func (set DecisionSet) AllowsCapability(action Action, resource Resource) bool {
	for _, decision := range set.decisions {
		if decision.allowed && decision.capability.Action == action && decision.capability.Resource == resource {
			return true
		}
	}
	return false
}

type Relationship uint8

const (
	RelationshipOwner Relationship = iota + 1
	RelationshipCollaborator
	RelationshipEditingCollaborator
	RelationshipProjectMember
	RelationshipProjectEditor
)

type Relationships struct{ bits uint8 }

func NewRelationships(values ...Relationship) (Relationships, error) {
	var relationships Relationships
	for _, relationship := range values {
		if relationship < RelationshipOwner || relationship > RelationshipProjectEditor {
			return Relationships{}, ErrAuthorizationResource
		}
		relationships.bits |= 1 << (relationship - 1)
	}
	return relationships, nil
}

func (relationships Relationships) Has(relationship Relationship) bool {
	return relationship >= RelationshipOwner && relationship <= RelationshipProjectEditor &&
		relationships.bits&(1<<(relationship-1)) != 0
}

type ResourceFacts struct {
	organization  OrganizationID
	resource      Resource
	id            string
	relationships Relationships
	published     bool
	system        bool
	valid         bool
}

func NewResourceFacts(
	organization OrganizationID,
	resource Resource,
	id string,
	relationships Relationships,
	published bool,
	system bool,
) (ResourceFacts, error) {
	if !organization.Valid() || resource.String() == "" || resource == ResourceOrganization ||
		resource == ResourceAdministration || !validResourceID(id) {
		return ResourceFacts{}, ErrAuthorizationResource
	}
	if system && resource != ResourceProject {
		return ResourceFacts{}, ErrAuthorizationResource
	}
	return ResourceFacts{
		organization:  organization,
		resource:      resource,
		id:            id,
		relationships: relationships,
		published:     published,
		system:        system,
		valid:         true,
	}, nil
}

func validResourceID(value string) bool {
	if len(value) < 1 || len(value) > 256 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("-._~:", character)) {
			return false
		}
	}
	return true
}

func (facts ResourceFacts) OrganizationID() OrganizationID { return facts.organization }
func (facts ResourceFacts) Resource() Resource             { return facts.resource }
func (facts ResourceFacts) ID() string                     { return facts.id }

func (facts ResourceFacts) subject() SubjectKey {
	if !facts.valid {
		return SubjectKey{}
	}
	return SubjectKey{value: fmt.Sprintf("resource/%s/%s/%s", facts.organization.String(), facts.resource.String(), facts.id)}
}

type VerifiedShare struct {
	organization OrganizationID
	resource     Resource
	resourceID   string
	media        bool
	expiresAt    time.Time
	valid        bool
}

func NewVerifiedShare(
	organization OrganizationID,
	resource Resource,
	resourceID string,
	media bool,
	expiresAt time.Time,
) (VerifiedShare, error) {
	if !organization.Valid() || !shareableResource(resource) || !validResourceID(resourceID) || expiresAt.IsZero() {
		return VerifiedShare{}, ErrAuthorizationScope
	}
	return VerifiedShare{
		organization: organization, resource: resource, resourceID: resourceID,
		media: media, expiresAt: expiresAt, valid: true,
	}, nil
}

func (share VerifiedShare) ValidAt(now time.Time) bool {
	return share.valid && !now.IsZero() && share.expiresAt.After(now)
}

type WorkerAssignment struct {
	organization  OrganizationID
	resource      Resource
	resourceID    string
	jobID         string
	attempt       uint32
	fence         uint64
	inputRevision uint64
	valid         bool
}

type WorkerResultScope struct {
	organization  OrganizationID
	resource      Resource
	resourceID    string
	jobID         string
	attempt       uint32
	fence         uint64
	inputRevision uint64
	valid         bool
}

func NewWorkerAssignment(
	organization OrganizationID,
	resource Resource,
	resourceID string,
	jobID string,
	attempt uint32,
	fence uint64,
	inputRevision uint64,
) (WorkerAssignment, error) {
	if !organization.Valid() || !validResourceID(resourceID) || !validResourceID(jobID) ||
		!workerResource(resource) || attempt == 0 || fence == 0 || inputRevision == 0 {
		return WorkerAssignment{}, ErrAuthorizationScope
	}
	return WorkerAssignment{
		organization: organization, resource: resource, resourceID: resourceID,
		jobID: jobID, attempt: attempt, fence: fence, inputRevision: inputRevision, valid: true,
	}, nil
}

func NewWorkerResultScope(
	organization OrganizationID,
	resource Resource,
	resourceID string,
	jobID string,
	attempt uint32,
	fence uint64,
	inputRevision uint64,
) (WorkerResultScope, error) {
	if !organization.Valid() || !validResourceID(resourceID) || !validResourceID(jobID) ||
		!workerResource(resource) || attempt == 0 || fence == 0 || inputRevision == 0 {
		return WorkerResultScope{}, ErrAuthorizationScope
	}
	return WorkerResultScope{
		organization: organization, resource: resource, resourceID: resourceID,
		jobID: jobID, attempt: attempt, fence: fence, inputRevision: inputRevision, valid: true,
	}, nil
}

type authorityKind uint8

const (
	authorityMember authorityKind = iota + 1
	authorityShare
	authorityWorker
)

type policyAuthority struct {
	kind   authorityKind
	member LocalPrincipal
	share  VerifiedShare
	worker WorkerAssignment
	result WorkerResultScope
	now    time.Time
}

type policyFacts struct {
	organization  OrganizationID
	resource      Resource
	scope         AuthorizationScope
	id            string
	relationships Relationships
	published     bool
	system        bool
	valid         bool
}

type capabilityDefinition struct {
	capability Capability
	evaluate   func(policyAuthority, policyFacts) bool
}

var capabilityDefinitions = buildCapabilityDefinitions()

func CapabilityCatalog() []Capability {
	capabilities := make([]Capability, 0, len(capabilityDefinitions))
	for _, definition := range capabilityDefinitions {
		capabilities = append(capabilities, definition.capability)
	}
	return capabilities
}

func ContextDecisions(principal LocalPrincipal) DecisionSet {
	authority := policyAuthority{kind: authorityMember, member: principal, now: time.Now()}
	organization := principal.OrganizationID()
	decisions := make([]Decision, 0)
	for _, definition := range capabilityDefinitions {
		if definition.capability.Scope != ScopeContext {
			continue
		}
		facts := policyFacts{
			organization: organization,
			resource:     definition.capability.Resource,
			scope:        ScopeContext,
			valid:        organization.Valid(),
		}
		decisions = append(decisions, evaluateDecision(definition, authority, facts))
	}
	return DecisionSet{decisions: decisions}
}

// AllowsAuthenticationSession evaluates access to the member's own session.
func AllowsAuthenticationSession(principal LocalPrincipal, action Action, owner UserID) bool {
	return (action == ActionRead || action == ActionSubscribe) && owner.Valid() && principal.UserID() == owner &&
		ContextDecisions(principal).AllowsCapability(ActionRead, ResourceOrganization)
}

func ResourceDecisions(principal LocalPrincipal, facts ResourceFacts) DecisionSet {
	return decisionsFor(policyAuthority{kind: authorityMember, member: principal, now: time.Now()}, facts)
}

func ShareDecisions(share VerifiedShare, facts ResourceFacts, now time.Time) DecisionSet {
	return decisionsFor(policyAuthority{kind: authorityShare, share: share, now: now}, facts)
}

func WorkerDecisions(assignment WorkerAssignment, result WorkerResultScope, facts ResourceFacts) DecisionSet {
	return decisionsFor(policyAuthority{kind: authorityWorker, worker: assignment, result: result}, facts)
}

func decisionsFor(authority policyAuthority, resource ResourceFacts) DecisionSet {
	facts := policyFacts{
		organization:  resource.organization,
		resource:      resource.resource,
		scope:         ScopeResource,
		id:            resource.id,
		relationships: resource.relationships,
		published:     resource.published,
		system:        resource.system,
		valid:         resource.valid,
	}
	decisions := make([]Decision, 0)
	for _, definition := range capabilityDefinitions {
		if definition.capability.Scope == ScopeResource && definition.capability.Resource == resource.resource {
			decisions = append(decisions, evaluateDecision(definition, authority, facts))
		}
	}
	return DecisionSet{decisions: decisions}
}

func evaluateDecision(definition capabilityDefinition, authority policyAuthority, facts policyFacts) Decision {
	decision := Decision{
		capability: definition.capability,
		subject:    subjectFor(facts),
		reason:     ReasonDenied,
	}
	if !facts.valid || decision.subject.String() == "" {
		decision.reason = ReasonUnsupportedCapability
		return decision
	}
	if !authorityValid(authority) {
		decision.reason = ReasonInvalidAuthority
		return decision
	}
	if authorityOrganization(authority) != facts.organization {
		decision.reason = ReasonCrossTenant
		return decision
	}
	if facts.system && facts.resource == ResourceProject &&
		(definition.capability.Action == ActionUpdate || definition.capability.Action == ActionDelete) {
		decision.reason = ReasonSystemResource
		return decision
	}
	decision.allowed = definition.evaluate(authority, facts)
	if decision.allowed {
		decision.reason = ReasonAllowed
	}
	return decision
}

func subjectFor(facts policyFacts) SubjectKey {
	if !facts.valid || !facts.organization.Valid() || facts.resource.String() == "" {
		return SubjectKey{}
	}
	if facts.scope == ScopeContext {
		return SubjectKey{value: fmt.Sprintf("context/%s/%s", facts.organization.String(), facts.resource.String())}
	}
	if facts.scope == ScopeResource && validResourceID(facts.id) {
		return SubjectKey{value: fmt.Sprintf("resource/%s/%s/%s", facts.organization.String(), facts.resource.String(), facts.id)}
	}
	return SubjectKey{}
}

func authorityValid(authority policyAuthority) bool {
	switch authority.kind {
	case authorityMember:
		return authority.member.ValidAt(authority.now)
	case authorityShare:
		return authority.share.ValidAt(authority.now)
	case authorityWorker:
		return authority.worker.valid && authority.result.valid
	default:
		return false
	}
}

func authorityOrganization(authority policyAuthority) OrganizationID {
	switch authority.kind {
	case authorityMember:
		return authority.member.OrganizationID()
	case authorityShare:
		return authority.share.organization
	case authorityWorker:
		return authority.worker.organization
	default:
		return OrganizationID{}
	}
}

func buildCapabilityDefinitions() []capabilityDefinition {
	definitions := []capabilityDefinition{
		contextCapability(ActionRead, ResourceOrganization, memberAny),
		contextCapability(ActionManageMembers, ResourceAdministration, memberAdmin),
		contextCapability(ActionRead, ResourceAdministration, deny),
	}
	for _, resource := range []Resource{
		ResourceProject, ResourceMeeting, ResourceRecording, ResourceHighlight, ResourcePlaylist,
	} {
		definitions = append(definitions, contextCapability(ActionCreate, resource, memberEditor))
	}
	for _, resource := range []Resource{
		ResourceProject, ResourceMeeting, ResourceRecording, ResourceTranscript,
		ResourceHighlight, ResourcePlaylist, ResourceShare,
	} {
		definitions = append(definitions,
			resourceCapability(ActionRead, resource, readable),
			resourceCapability(ActionUpdate, resource, mutable),
			resourceCapability(ActionDelete, resource, ownerAdministration),
			resourceCapability(ActionSearch, resource, memberReadable),
			resourceCapability(ActionSubscribe, resource, memberReadable),
		)
	}
	for _, resource := range []Resource{ResourceProject, ResourceMeeting, ResourcePlaylist} {
		definitions = append(definitions, resourceCapability(ActionManageCollaborators, resource, ownerAdministration))
	}
	for _, resource := range []Resource{ResourceMeeting, ResourceHighlight, ResourcePlaylist} {
		definitions = append(definitions,
			resourceCapability(ActionPublish, resource, ownerAdministration),
			resourceCapability(ActionManageShares, resource, ownerAdministration),
		)
	}
	for _, resource := range []Resource{ResourceMeeting, ResourceRecording, ResourceHighlight, ResourcePlaylist} {
		definitions = append(definitions, resourceCapability(ActionIssueMediaGrant, resource, mediaReadable))
	}
	for _, resource := range []Resource{ResourceMeeting, ResourceRecording, ResourceTranscript, ResourceHighlight} {
		definitions = append(definitions, resourceCapability(ActionEnqueueWork, resource, mutable))
	}
	for _, resource := range []Resource{ResourceRecording, ResourceTranscript, ResourceHighlight} {
		definitions = append(definitions, resourceCapability(ActionSubmitWorkerResult, resource, assignedWorker))
	}
	return definitions
}

func contextCapability(action Action, resource Resource, evaluate func(policyAuthority, policyFacts) bool) capabilityDefinition {
	return capabilityDefinition{capability: Capability{Action: action, Resource: resource, Scope: ScopeContext}, evaluate: evaluate}
}

func resourceCapability(action Action, resource Resource, evaluate func(policyAuthority, policyFacts) bool) capabilityDefinition {
	return capabilityDefinition{capability: Capability{Action: action, Resource: resource, Scope: ScopeResource}, evaluate: evaluate}
}

func deny(policyAuthority, policyFacts) bool { return false }

func memberAny(authority policyAuthority, _ policyFacts) bool {
	return authority.kind == authorityMember
}

func memberAdmin(authority policyAuthority, _ policyFacts) bool {
	return authority.kind == authorityMember && authority.member.Roles().Has(RoleAdmin)
}

func memberEditor(authority policyAuthority, _ policyFacts) bool {
	return authority.kind == authorityMember &&
		(authority.member.Roles().Has(RoleAdmin) || authority.member.Roles().Has(RoleEditor))
}

func readable(authority policyAuthority, facts policyFacts) bool {
	switch authority.kind {
	case authorityMember:
		return memberReadable(authority, facts)
	case authorityShare:
		return authority.share.resource == facts.resource && authority.share.resourceID == facts.id
	default:
		return false
	}
}

func memberReadable(authority policyAuthority, facts policyFacts) bool {
	if authority.kind != authorityMember {
		return false
	}
	if authority.member.Roles().Has(RoleAdmin) {
		return true
	}
	return facts.published || facts.relationships.Has(RelationshipOwner) ||
		facts.relationships.Has(RelationshipCollaborator) ||
		facts.relationships.Has(RelationshipEditingCollaborator) ||
		facts.relationships.Has(RelationshipProjectMember) ||
		facts.relationships.Has(RelationshipProjectEditor)
}

func mediaReadable(authority policyAuthority, facts policyFacts) bool {
	if authority.kind == authorityShare {
		return authority.share.media && authority.share.resource == facts.resource && authority.share.resourceID == facts.id
	}
	return memberReadable(authority, facts)
}

func mutable(authority policyAuthority, facts policyFacts) bool {
	if authority.kind != authorityMember {
		return false
	}
	if authority.member.Roles().Has(RoleAdmin) {
		return true
	}
	if !authority.member.Roles().Has(RoleEditor) {
		return false
	}
	return facts.relationships.Has(RelationshipOwner) ||
		facts.relationships.Has(RelationshipEditingCollaborator) ||
		facts.relationships.Has(RelationshipProjectEditor)
}

func ownerAdministration(authority policyAuthority, facts policyFacts) bool {
	if authority.kind != authorityMember {
		return false
	}
	return authority.member.Roles().Has(RoleAdmin) ||
		(authority.member.Roles().Has(RoleEditor) && facts.relationships.Has(RelationshipOwner))
}

func assignedWorker(authority policyAuthority, facts policyFacts) bool {
	return authority.kind == authorityWorker && authority.worker.resource == facts.resource &&
		authority.worker.resourceID == facts.id && authority.worker.jobID != "" &&
		authority.worker.organization == authority.result.organization &&
		authority.worker.resource == authority.result.resource &&
		authority.worker.resourceID == authority.result.resourceID &&
		authority.worker.jobID == authority.result.jobID &&
		authority.worker.attempt == authority.result.attempt &&
		authority.worker.fence == authority.result.fence &&
		authority.worker.inputRevision == authority.result.inputRevision
}

func shareableResource(resource Resource) bool {
	return resource == ResourceMeeting || resource == ResourceHighlight || resource == ResourcePlaylist
}

func workerResource(resource Resource) bool {
	return resource == ResourceRecording || resource == ResourceTranscript || resource == ResourceHighlight
}
