import Link from "next/link";

import { buttonVariants } from "@/components/ui/button";

export default function NotFoundPage() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-background px-6">
      <div className="max-w-lg text-center" data-enter>
        <p className="text-sm font-medium text-muted-foreground">404</p>
        <h1 className="mt-4 text-4xl font-medium tracking-[-0.04em]">Page not found</h1>
        <p className="mt-4 text-base leading-7 text-muted-foreground">The address may be old, or the page may have moved to a different workspace.</p>
        <Link className={buttonVariants({ className: "mt-7", variant: "primary" })} href="/meetings">
          See meetings
        </Link>
      </div>
    </main>
  );
}
