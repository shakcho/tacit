import { LoaderCircle, ShieldCheck } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";

import { BrandMark } from "@/components/brand-mark";
import { buttonVariants } from "@/components/ui/button";

export const metadata: Metadata = {
  robots: { follow: false, index: false },
  title: "Authentication callback",
};

export default function AuthenticationCallbackPage() {
  return (
    <main className="grid min-h-screen place-items-center bg-background px-5 py-12">
      <section className="w-full max-w-md rounded-xl border border-border bg-card p-8 text-center" data-enter>
        <BrandMark className="mx-auto size-10" />
        <span className="mx-auto mt-8 inline-flex size-11 items-center justify-center rounded-full bg-muted text-muted-foreground">
          <LoaderCircle aria-hidden="true" className="size-5 animate-spin motion-reduce:animate-none" />
        </span>
        <h1 className="mt-5 text-2xl font-medium tracking-tight">Completing sign in</h1>
        <p className="mt-3 text-base leading-7 text-muted-foreground">The selected identity provider will verify the callback before Tacit creates a session.</p>
        <div className="mt-7 rounded-lg border border-border bg-muted p-4 text-left">
          <p className="flex items-center gap-2 text-sm font-medium">
            <ShieldCheck aria-hidden="true" className="size-4 text-primary" />
            Session checks stay at the boundary
          </p>
        </div>
        <Link className={buttonVariants({ className: "mt-7 w-full", variant: "secondary" })} href="/meetings">Return to meetings</Link>
      </section>
    </main>
  );
}
