import { ShieldAlert } from "lucide-react";
import type { Metadata } from "next";

import { BrandMark } from "@/components/brand-mark";
import { buttonVariants } from "@/components/ui/button";

export const metadata: Metadata = {
  robots: { follow: false, index: false },
  title: "Workspace access",
};

type AccessPageProps = Readonly<{
  params: Promise<{ reason: string }>;
}>;

export default async function AccessPage({ params }: AccessPageProps) {
  const unavailable = (await params).reason === "unavailable";
  return (
    <main className="grid min-h-screen place-items-center bg-background px-5 py-12">
      <section className="w-full max-w-lg rounded-xl border border-border bg-card p-8" data-enter>
        <BrandMark className="size-10" />
        <span className="mt-8 inline-flex size-11 items-center justify-center rounded-full bg-muted text-muted-foreground">
          <ShieldAlert aria-hidden="true" className="size-5" />
        </span>
        <h1 className="mt-5 text-2xl font-medium tracking-tight">
          {unavailable ? "Workspace access is temporarily unavailable" : "You do not have access to this workspace"}
        </h1>
        <p className="mt-3 text-base leading-7 text-muted-foreground">
          {unavailable
            ? "Tacit could not safely confirm your local membership. Try again after the API and database are available."
            : "Your identity is verified, but it is not linked to an active Tacit organization membership. Ask a workspace administrator for an invitation."}
        </p>
        <div className="mt-6 flex flex-wrap gap-3">
          <a className={buttonVariants()} href={unavailable ? "/meetings" : "/auth/sign-in"}>
            {unavailable ? "Try again" : "Connect workspace"}
          </a>
          <form action="/auth/sign-out" method="post">
            <button className={buttonVariants({ variant: "secondary" })} type="submit">Sign out</button>
          </form>
        </div>
      </section>
    </main>
  );
}
