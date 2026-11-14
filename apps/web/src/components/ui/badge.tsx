import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";

import { cn } from "@/lib/cn";

const badgeVariants = cva(
  "inline-flex min-h-6 items-center rounded-full border px-2.5 text-xs font-medium",
  {
    defaultVariants: { tone: "neutral" },
    variants: {
      tone: {
        attention: "border-amber-300 bg-amber-50 text-amber-950",
        neutral: "border-border bg-muted text-muted-foreground",
        positive: "border-emerald-300 bg-emerald-50 text-emerald-950",
      },
    },
  },
);

type BadgeProps = ComponentProps<"span"> & VariantProps<typeof badgeVariants>;

export function Badge({ className, tone, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ className, tone }))} {...props} />;
}
