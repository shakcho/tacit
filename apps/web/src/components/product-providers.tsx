"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";

import type { AuthenticationSession } from "@/api/generated/models";
import { getGetAuthenticationSessionQueryKey } from "@/api/generated/tacit";
import { AuthorizationProvider } from "@/auth/authorization-provider";
import { followSession } from "@/auth/session-events";
import { QueryProvider } from "@/components/query-provider";

type ProductProvidersProps = {
  session: AuthenticationSession;
  children: ReactNode;
};

export function ProductProviders({ session, children }: ProductProvidersProps) {
  return (
    <QueryProvider>
      <LiveAuthorization session={session}>{children}</LiveAuthorization>
    </QueryProvider>
  );
}

function LiveAuthorization({ session, children }: ProductProvidersProps) {
  const client = useQueryClient();
  const router = useRouter();
  const { data: current } = useQuery<AuthenticationSession | null>({
    queryKey: getGetAuthenticationSessionQueryKey(),
    enabled: false,
    initialData: session,
  });
  useEffect(() => {
    const lifetime = new AbortController();
    void followSession({
      client,
      initial: session,
      signal: lifetime.signal,
      onAccessEnded: (reason) => {
        if (reason === "scope_changed") router.refresh();
        else router.replace("/auth/access/not-admitted");
      },
    });
    return () => lifetime.abort();
  }, [client, router, session]);

  if (current === null) {
    return <p className="p-6 text-sm text-muted-foreground" role="status">Checking workspace access…</p>;
  }
  return <AuthorizationProvider snapshot={current.authorization}>{children}</AuthorizationProvider>;
}
