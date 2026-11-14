import { Clock3, LockKeyhole } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";

import { BrandMark } from "@/components/brand-mark";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";

export const metadata: Metadata = {
  description: "A meeting item shared from Tacit.",
  robots: { follow: false, index: false },
  title: "Shared meeting item",
};

type PublicSharePageProps = {
  params: Promise<{ shareId: string }>;
};

export default async function PublicSharePage({ params }: PublicSharePageProps) {
  await params;

  return (
    <div className="min-h-screen bg-background text-foreground">
      <a className="fixed left-4 top-3 z-50 -translate-y-24 rounded-md bg-foreground px-4 py-3 text-sm font-medium text-background transition-transform focus:translate-y-0 focus:outline-none focus:ring-2 focus:ring-ring" href="#shared-content">
        Skip to shared content
      </a>
      <header className="border-b border-border bg-background">
        <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-5 sm:px-8">
          <Link className="flex min-h-11 items-center gap-3 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" href="/meetings">
            <BrandMark />
            <span className="font-medium tracking-tight">Tacit</span>
          </Link>
          <Badge tone="neutral">
            <LockKeyhole aria-hidden="true" className="mr-1 size-3.5" />
            Shared view
          </Badge>
        </div>
      </header>

      <main className="mx-auto grid min-h-[calc(100vh-4rem)] max-w-6xl content-center gap-8 px-5 py-12 sm:px-8 lg:grid-cols-[minmax(0,1fr)_22rem]" id="shared-content">
        <section data-enter>
          <p className="text-sm font-medium text-muted-foreground">Public share</p>
          <h1 className="mt-4 max-w-2xl text-4xl font-medium tracking-[-0.04em] sm:text-5xl">Shared meeting review</h1>
          <p className="mt-5 max-w-xl text-base leading-7 text-muted-foreground">This route is ready for a revocable meeting, highlight, or playlist share. No source media is exposed by the shell.</p>
          <div className="mt-8 flex flex-col gap-3 sm:flex-row">
            <Link className={buttonVariants({ variant: "primary" })} href="/meetings">Open Tacit</Link>
          </div>
        </section>

        <aside className="rounded-xl border border-border bg-card p-6" aria-labelledby="share-status-title">
          <span className="inline-flex size-10 items-center justify-center rounded-lg bg-muted text-muted-foreground">
            <Clock3 aria-hidden="true" className="size-[18px]" />
          </span>
          <h2 className="mt-5 text-lg font-medium" id="share-status-title">Waiting for shared content</h2>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">The sharing module will resolve the capability, expiry, and authorized media manifest before anything renders.</p>
        </aside>
      </main>
    </div>
  );
}
