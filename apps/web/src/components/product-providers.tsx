"use client";

import type { ReactNode } from "react";

import type { AuthorizationSnapshot } from "@/api/generated/models";
import { AuthorizationProvider } from "@/auth/authorization-provider";
import { QueryProvider } from "@/components/query-provider";

type ProductProvidersProps = {
  authorization: AuthorizationSnapshot;
  children: ReactNode;
};

export function ProductProviders({ authorization, children }: ProductProvidersProps) {
  return (
    <QueryProvider>
      <AuthorizationProvider snapshot={authorization}>{children}</AuthorizationProvider>
    </QueryProvider>
  );
}
