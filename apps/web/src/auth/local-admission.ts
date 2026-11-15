import {
  AuthorizationAction,
  AuthorizationResource,
  AuthorizationScope,
  MembershipRole,
  type AuthenticationSession,
  type AuthorizationDecision,
} from "@/api/generated/models";
import { getServerEnvironment } from "@/env/server";

export type LocalAdmission =
  | { kind: "admitted"; session: AuthenticationSession }
  | { kind: "not-admitted" }
  | { kind: "unavailable" };

export async function localAdmission(accessToken: string | undefined): Promise<LocalAdmission> {
  if (accessToken === undefined) return { kind: "not-admitted" };

  let response: Response;
  try {
    response = await fetch(new URL("/v1/auth/session", getServerEnvironment().apiOrigin), {
      cache: "no-store",
      headers: { Authorization: `Bearer ${accessToken}` },
      redirect: "manual",
      signal: AbortSignal.timeout(10_000),
    });
  } catch {
    return { kind: "unavailable" };
  }
  if (response.status === 401 || response.status === 403) return { kind: "not-admitted" };
  if (!response.ok) return { kind: "unavailable" };
  try {
    const session = parseAuthenticationSession(await response.json());
    return session === undefined ? { kind: "unavailable" } : { kind: "admitted", session };
  } catch {
    return { kind: "unavailable" };
  }
}

function parseAuthenticationSession(input: unknown): AuthenticationSession | undefined {
  if (!isRecord(input) || typeof input.userId !== "string" || typeof input.organizationId !== "string" ||
      !/^usr_[0-9a-f]{32}$/.test(input.userId) || !/^org_[0-9a-f]{32}$/.test(input.organizationId) ||
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

function isRecord(input: unknown): input is Record<string, unknown> {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}
