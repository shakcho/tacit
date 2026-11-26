import type { ReactNode } from "react";
import { withAuth } from "@workos-inc/authkit-nextjs";
import { redirect } from "next/navigation";

import { localAdmission } from "@/auth/local-admission";
import { AppShell } from "@/components/app-shell";
import { ProductProviders } from "@/components/product-providers";

type ProductLayoutProps = {
  children: ReactNode;
};

export default async function ProductLayout({ children }: ProductLayoutProps) {
  const auth = await withAuth({ ensureSignedIn: true });
  const admission = await localAdmission(auth.accessToken);
  if (admission.kind === "not-admitted") redirect("/auth/access/not-admitted");
  if (admission.kind === "unavailable") redirect("/auth/access/unavailable");
  const displayName = [auth.user.firstName, auth.user.lastName].filter((part) => part !== undefined).join(" ");

  return (
    <ProductProviders
      session={admission.session}
      key={`${admission.session.userId}:${admission.session.organizationId}`}
    >
      <AppShell
        account={{
          email: auth.user.email,
          name: displayName.length > 0 ? displayName : auth.user.email,
          organization: admission.session.organizationId,
        }}
      >
        {children}
      </AppShell>
    </ProductProviders>
  );
}
