import { CircleHelp, MonitorUp, Search, Upload, Video } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";

import { DesktopNavigation, MobileNavigation } from "@/components/app-navigation";
import { BrandMark } from "@/components/brand-mark";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/cn";

type AppShellProps = {
  account: Readonly<{
    email: string;
    name: string;
    organization: string;
  }>;
  children: ReactNode;
};

function initials(name: string): string {
  const parts = name.trim().split(/\s+/u).filter((part) => part.length > 0);
  return parts.slice(0, 2).map((part) => part[0]?.toUpperCase() ?? "").join("") || "U";
}

export function AppShell({ account, children }: AppShellProps) {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <a
        className="fixed left-4 top-3 z-50 -translate-y-24 rounded-md bg-foreground px-4 py-3 text-sm font-medium text-background transition-transform focus:translate-y-0 focus:outline-none focus:ring-2 focus:ring-ring motion-reduce:transition-none"
        href="#main-content"
      >
        Skip to content
      </a>

      <aside className="fixed inset-y-0 left-0 hidden w-[15.5rem] border-r border-sidebar-border bg-sidebar p-4 text-sidebar-foreground md:flex md:flex-col">
        <Link className="flex min-h-12 items-center gap-3 rounded-lg px-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring" href="/meetings">
          <BrandMark className="size-9" />
          <span className="text-xl font-semibold tracking-[-0.04em]">tacit</span>
        </Link>

        <Link
          className="mt-5 flex min-h-11 items-center gap-3 rounded-lg border border-sidebar-border bg-card px-3 text-sm text-sidebar-muted transition-colors hover:border-primary/30 hover:text-sidebar-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring"
          href="/search"
        >
          <Search aria-hidden="true" className="size-[18px]" strokeWidth={1.8} />
          <span>Search</span>
          <kbd className="ml-auto rounded-md bg-muted px-2 py-1 text-[0.6875rem] font-medium text-muted-foreground">⌘K</kbd>
        </Link>

        <DesktopNavigation />

        <section aria-labelledby="sources-title" className="mt-auto rounded-xl bg-sidebar-panel p-3">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-xs font-semibold text-sidebar-foreground" id="sources-title">Meeting sources</h2>
            <span className="text-[0.6875rem] text-sidebar-muted">setup</span>
          </div>
          <div className="space-y-2.5 text-sm">
            <div className="flex items-center gap-2.5">
              <Video aria-hidden="true" className="size-4 text-primary" strokeWidth={1.8} />
              <span>Zoom</span>
              <span className="ml-auto text-xs text-sidebar-muted">planned</span>
            </div>
            <div className="flex items-center gap-2.5">
              <MonitorUp aria-hidden="true" className="size-4 text-primary" strokeWidth={1.8} />
              <span>Microsoft Teams</span>
              <span className="ml-auto text-xs text-sidebar-muted">planned</span>
            </div>
            <div className="flex items-center gap-2.5">
              <Upload aria-hidden="true" className="size-4 text-primary" strokeWidth={1.8} />
              <span>File uploads</span>
              <span className="ml-auto inline-flex items-center gap-1.5 text-xs text-sidebar-muted">
                <span aria-hidden="true" className="size-1.5 rounded-full bg-emerald-500" /> ready
              </span>
            </div>
          </div>
        </section>

        <div className="mt-3 space-y-1 border-t border-sidebar-border pt-3">
          <Link className="flex min-h-11 items-center gap-3 rounded-lg px-3 text-sm font-medium text-sidebar-muted hover:bg-sidebar-hover hover:text-sidebar-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring" href="/help">
            <CircleHelp aria-hidden="true" className="size-[18px]" strokeWidth={1.8} />
            Help
          </Link>
          <Link className="flex min-h-11 items-center gap-3 rounded-lg px-3 text-sm font-medium text-sidebar-muted hover:bg-sidebar-hover hover:text-sidebar-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring" href="/profile">
            <span aria-hidden="true" className="inline-flex size-8 items-center justify-center rounded-lg bg-foreground text-xs font-semibold text-background">{initials(account.name)}</span>
            <span>
              <span className="block max-w-36 truncate text-sm text-sidebar-foreground">{account.name}</span>
              <span className="block max-w-36 truncate text-xs font-normal text-sidebar-muted">{account.organization}</span>
            </span>
          </Link>
        </div>
      </aside>

      <div className="md:pl-[15.5rem]">
        <header className="sticky top-0 z-20 flex h-16 items-center justify-between border-b border-border bg-card/95 px-4 backdrop-blur sm:px-6 md:hidden">
          <Link className="flex min-h-11 items-center gap-2 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:hidden" href="/meetings">
            <BrandMark className="size-7" />
            <span className="font-semibold tracking-[-0.03em]">tacit</span>
          </Link>
          <div className="flex items-center gap-2">
            <Link
              aria-label="Search workspace"
              className={cn(buttonVariants({ size: "icon", variant: "ghost" }), "rounded-full")}
              href="/search"
            >
              <Search aria-hidden="true" className="size-[18px]" />
            </Link>
            <Link
              aria-label="Open account"
              className="inline-flex size-10 items-center justify-center rounded-full border border-border bg-card text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              href="/profile"
            >
              {initials(account.name).slice(0, 1)}
            </Link>
          </div>
        </header>

        <main className="mx-auto min-h-screen max-w-[100rem] px-4 py-7 pb-28 sm:px-6 md:px-8 md:py-9 md:pb-12 xl:px-12" id="main-content" tabIndex={-1}>
          {children}
        </main>
      </div>

      <MobileNavigation />
    </div>
  );
}
