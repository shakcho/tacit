"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";

import type { AuthorizationSnapshot } from "@/api/generated/models";
import { createPresentationAbility, type AppAbility } from "@/auth/abilities";

const AuthorizationContext = createContext<AppAbility | undefined>(undefined);

type AuthorizationProviderProps = {
  children: ReactNode;
  snapshot: AuthorizationSnapshot;
};

export function AuthorizationProvider({ children, snapshot }: AuthorizationProviderProps) {
  const ability = useMemo(() => createPresentationAbility(snapshot), [snapshot]);
  return <AuthorizationContext.Provider value={ability}>{children}</AuthorizationContext.Provider>;
}

export function useAuthorization(): AppAbility {
  const ability = useContext(AuthorizationContext);
  if (ability === undefined) throw new Error("AuthorizationProvider is required");
  return ability;
}
