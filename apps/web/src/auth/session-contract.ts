import {
  AuthorizationAction,
  AuthorizationResource,
  AuthorizationScope,
  MembershipRole,
  type AuthenticationSession,
  type AuthorizationDecision,
} from "@/api/generated/models";

export function isRealtimeCursor(input: unknown): input is AuthenticationSession["cursor"] {
  return typeof input === "string" && /^v1\.[A-Za-z0-9_-]{75}$/u.test(input);
}

export function isSessionRevision(input: unknown): input is AuthenticationSession["revision"] {
  return typeof input === "string" && /^(0|[1-9][0-9]{0,18})$/u.test(input) &&
    (input.length < 19 || input <= "9223372036854775807");
}

export function isSessionUserId(input: unknown): input is AuthenticationSession["userId"] {
  return typeof input === "string" && /^usr_[0-9a-f]{32}$/u.test(input);
}

export function parseAuthenticationSession(input: unknown): AuthenticationSession | undefined {
  if (!isRecord(input) || !isSessionUserId(input.userId) || typeof input.organizationId !== "string" ||
      !/^org_[0-9a-f]{32}$/.test(input.organizationId) ||
      !isRealtimeCursor(input.cursor) || !isSessionRevision(input.revision) ||
      !Array.isArray(input.roles) || input.roles.length === 0 || !isRecord(input.authorization) ||
      typeof input.authorization.policyRevision !== "string" || input.authorization.policyRevision.length === 0 ||
      !Array.isArray(input.authorization.decisions)) {
    return undefined;
  }
  const roles = input.roles.filter(isMembershipRole);
  if (roles.length !== input.roles.length || new Set(roles).size !== roles.length) return undefined;
  const decisions: AuthorizationDecision[] = [];
  for (const value of input.authorization.decisions) {
    const decision = parseDecision(value, input.organizationId);
    if (decision === undefined) return undefined;
    decisions.push(decision);
  }
  const decisionKeys = decisions.map((decision) => `${decision.action}:${decision.subject}`);
  if (new Set(decisionKeys).size !== decisionKeys.length) return undefined;
  return {
    userId: input.userId,
    organizationId: input.organizationId,
    cursor: input.cursor,
    revision: input.revision,
    roles,
    authorization: {
      policyRevision: input.authorization.policyRevision,
      decisions,
    },
  };
}

function parseDecision(input: unknown, organizationId: string): AuthorizationDecision | undefined {
  if (!isRecord(input) || !isEnumValue(AuthorizationAction, input.action) ||
      !isEnumValue(AuthorizationResource, input.resource) || !isEnumValue(AuthorizationScope, input.scope) ||
      typeof input.subject !== "string" || !/^(context|resource)\//.test(input.subject) ||
      typeof input.allowed !== "boolean") {
    return undefined;
  }
  const expectedSubject = input.scope === AuthorizationScope.context
    ? `context/${organizationId}/${input.resource}`
    : `resource/${organizationId}/${input.resource}/`;
  const validSubject = input.scope === AuthorizationScope.context
    ? input.subject === expectedSubject
    : input.subject.startsWith(expectedSubject) && isResourceId(input.subject.slice(expectedSubject.length));
  if (!validSubject) {
    return undefined;
  }
  return {
    action: input.action,
    resource: input.resource,
    scope: input.scope,
    subject: input.subject,
    allowed: input.allowed,
  };
}

function isResourceId(input: string): boolean {
  return input.length >= 1 && input.length <= 256 && /^[A-Za-z0-9._~:-]+$/.test(input);
}

function isMembershipRole(input: unknown): input is MembershipRole {
  return isEnumValue(MembershipRole, input);
}

function isEnumValue<const Values extends Record<string, string>>(
  values: Values,
  input: unknown,
): input is Values[keyof Values] {
  return typeof input === "string" && Object.values(values).includes(input);
}

export function isRecord(input: unknown): input is Record<string, unknown> {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}
