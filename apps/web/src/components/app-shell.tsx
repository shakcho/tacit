import { CircleHelp, Search } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";

import { DesktopNavigation, MobileNavigation } from "@/components/app-navigation";
import { BrandMark } from "@/components/brand-mark";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/cn";

type AppShellProps = {
  children: ReactNode;
};

export function AppShell({ children }: AppShellProps) {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <a
        className="fixed left-4 top-3 z-50 -translate-y-24 rounded-md bg-foreground px-4 py-3 text-sm font-medium text-background transition-transform focus:translate-y-0 focus:outline-none focus:ring-2 focus:ring-ring motion-reduce:transition-none"
        href="#main-content"
      >
        Skip to content
      </a>

      <aside className="fixed inset-y-0 left-0 hidden w-64 border-r border-sidebar-border bg-sidebar px-5 py-6 text-sidebar-foreground md:flex md:flex-col">
        <Link className="flex min-h-11 items-center gap-3 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring" href="/meetings">
          <BrandMark />
          <span className="text-lg font-medium tracking-[-0.025em]">Tacit</span>
        </Link>

        <div className="mt-6 rounded-lg border border-sidebar-border bg-sidebar-panel p-3">
          <p className="text-xs font-medium text-sidebar-muted">Workspace</p>
          <p className="mt-1 truncate text-sm font-medium">Tacit workspace</p>
        </div>

        <DesktopNavigation />

        <div className="mt-auto space-y-1 border-t border-sidebar-border pt-4">
          <Link className="flex min-h-11 items-center gap-3 rounded-lg px-3 text-sm font-medium text-sidebar-muted hover:bg-sidebar-hover hover:text-sidebar-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring" href="/help">
            <CircleHelp aria-hidden="true" className="size-[18px]" strokeWidth={1.8} />
            Help
          </Link>
          <Link className="flex min-h-11 items-center gap-3 rounded-lg px-3 text-sm font-medium text-sidebar-muted hover:bg-sidebar-hover hover:text-sidebar-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring" href="/profile">
            <span aria-hidden="true" className="inline-flex size-7 items-center justify-center rounded-full bg-sidebar-active text-xs text-sidebar-foreground">A</span>
            Account
          </Link>
        </div>
      </aside>

      <div className="md:pl-64">
        <header className="sticky top-0 z-20 flex h-16 items-center justify-between border-b border-border bg-background px-4 sm:px-6 lg:px-10">
          <Link className="flex min-h-11 items-center gap-2 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:hidden" href="/meetings">
            <BrandMark className="size-7" />
            <span className="font-medium tracking-tight">Tacit</span>
          </Link>
          <p className="hidden text-sm font-medium text-muted-foreground md:block">Meeting workspace</p>
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
              A
            </Link>
          </div>
        </header>

        <main className="mx-auto min-h-[calc(100vh-4rem)] max-w-[92rem] px-4 py-8 pb-28 sm:px-6 lg:px-10 lg:py-10 md:pb-10" id="main-content" tabIndex={-1}>
          {children}
        </main>
      </div>

      <MobileNavigation />
    </div>
  );
}
