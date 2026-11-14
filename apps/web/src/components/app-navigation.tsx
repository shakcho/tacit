"use client";

import {
  FolderKanban,
  ListVideo,
  MessagesSquare,
  PlaySquare,
  type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { cn } from "@/lib/cn";

type AppPath = "/highlights" | "/meetings" | "/playlists" | "/projects";

type NavigationItem = {
  href: AppPath;
  icon: LucideIcon;
  label: string;
};

const items = [
  { href: "/meetings", icon: MessagesSquare, label: "Meetings" },
  { href: "/highlights", icon: ListVideo, label: "Highlights" },
  { href: "/playlists", icon: PlaySquare, label: "Playlists" },
  { href: "/projects", icon: FolderKanban, label: "Projects" },
] satisfies readonly NavigationItem[];

function isCurrentPath(pathname: string, href: AppPath): boolean {
  return pathname === href || pathname.startsWith(`${href}/`);
}

export function DesktopNavigation() {
  const pathname = usePathname();

  return (
    <nav aria-label="Product" className="mt-3 space-y-1">
      {items.map((item) => {
        const current = isCurrentPath(pathname, item.href);
        const Icon = item.icon;

        return (
          <Link
            aria-current={current ? "page" : undefined}
            className={cn(
              "group flex min-h-11 items-center gap-3 rounded-lg px-3 text-[0.9375rem] font-medium text-sidebar-muted transition-[background-color,color,transform] hover:translate-x-0.5 hover:bg-sidebar-hover hover:text-sidebar-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring motion-reduce:transition-none",
              current && "bg-sidebar-active text-sidebar-foreground",
            )}
            href={item.href}
            key={item.href}
          >
            <Icon aria-hidden="true" className="size-[18px]" strokeWidth={1.8} />
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}

export function MobileNavigation() {
  const pathname = usePathname();

  return (
    <nav
      aria-label="Product"
      className="fixed inset-x-0 bottom-0 z-30 grid h-[calc(4.25rem+env(safe-area-inset-bottom))] grid-cols-4 border-t border-border bg-background px-2 pb-[env(safe-area-inset-bottom)] md:hidden"
    >
      {items.map((item) => {
        const current = isCurrentPath(pathname, item.href);
        const Icon = item.icon;

        return (
          <Link
            aria-current={current ? "page" : undefined}
            className={cn(
              "flex min-h-11 flex-col items-center justify-center gap-1 rounded-md text-[0.8125rem] font-medium text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              current && "text-foreground",
            )}
            href={item.href}
            key={item.href}
          >
            <Icon aria-hidden="true" className="size-5" strokeWidth={current ? 2.2 : 1.8} />
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}
