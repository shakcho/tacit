import type { ReactNode } from "react";
import { withAuth } from "@workos-inc/authkit-nextjs";
import { redirect } from "next/navigation";

import { localAdmission } from "@/auth/local-admission";
import { AppShell } from "@/components/app-shell";

type ProductLayoutProps = {
  children: ReactNode;
};

export default async function ProductLayout({ children }: ProductLayoutProps) {
  const auth = await withAuth({ ensureSignedIn: true });
  const admission = await localAdmission(auth.accessToken);
  if (admission === "not-admitted") redirect("/auth/access/not-admitted");
  if (admission === "unavailable") redirect("/auth/access/unavailable");
  const displayName = [auth.user.firstName, auth.user.lastName].filter((part) => part !== undefined).join(" ");

  return (
    <AppShell
      account={{
        email: auth.user.email,
        name: displayName.length > 0 ? displayName : auth.user.email,
        organization: auth.organizationId ?? "No organization selected",
      }}
    >
      {children}
    </AppShell>
  );
}
