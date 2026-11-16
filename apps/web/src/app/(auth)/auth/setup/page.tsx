import { ShieldAlert } from "lucide-react";
import type { Metadata } from "next";
import { redirect } from "next/navigation";

import { BrandMark } from "@/components/brand-mark";
import { hasWorkOSConfiguration } from "@/lib/workos-environment";

export const metadata: Metadata = {
  robots: { follow: false, index: false },
  title: "Configure identity",
};

export default function IdentitySetupPage() {
  if (hasWorkOSConfiguration()) redirect("/meetings");

  return (
    <main className="grid min-h-screen place-items-center bg-background px-5 py-12">
      <section className="w-full max-w-lg rounded-xl border border-border bg-card p-8" data-enter>
        <BrandMark className="size-10" />
        <span className="mt-8 inline-flex size-11 items-center justify-center rounded-full bg-muted text-muted-foreground">
          <ShieldAlert aria-hidden="true" className="size-5" />
        </span>
        <h1 className="mt-5 text-2xl font-medium tracking-tight">WorkOS is not configured</h1>
        <p className="mt-3 text-base leading-7 text-muted-foreground">
          Add the WorkOS client, cookie password, exact token issuer, and callback URI to <code>.env.local</code>, then restart the development stack.
        </p>
        <p className="mt-5 rounded-lg border border-border bg-muted p-4 text-sm leading-6 text-muted-foreground">
          Tacit fails closed instead of creating a local authentication bypass. Public share pages and health checks remain available.
        </p>
      </section>
    </main>
  );
}
