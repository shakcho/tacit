"use client";

import { AlertTriangle } from "lucide-react";

import { Button } from "@/components/ui/button";

type ProductErrorProps = {
  reset: () => void;
};

export default function ProductError({ reset }: ProductErrorProps) {
  return (
    <div className="flex min-h-[60vh] items-center justify-center">
      <div className="max-w-lg text-center">
        <span className="mx-auto inline-flex size-12 items-center justify-center rounded-xl bg-amber-50 text-amber-900">
          <AlertTriangle aria-hidden="true" className="size-5" />
        </span>
        <h1 className="mt-5 text-3xl font-medium tracking-tight">Workspace unavailable</h1>
        <p className="mt-3 text-base leading-7 text-muted-foreground">The workspace did not finish loading. Try the request again.</p>
        <Button className="mt-6" onClick={reset}>Try again</Button>
      </div>
    </div>
  );
}
