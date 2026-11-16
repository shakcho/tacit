import { Ability, type RawRuleFrom } from "@casl/ability";

import type { AuthorizationAction, AuthorizationSnapshot } from "@/api/generated/models";

export type AppAbility = Ability<[AuthorizationAction, string]>;

export function createPresentationAbility(snapshot: AuthorizationSnapshot): AppAbility {
  const rules: RawRuleFrom<[AuthorizationAction, string], never>[] = snapshot.decisions
    .filter((decision) => decision.allowed)
    .map((decision) => ({ action: decision.action, subject: decision.subject }));
  return new Ability<[AuthorizationAction, string]>(rules);
}
