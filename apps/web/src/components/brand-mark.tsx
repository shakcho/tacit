import { cn } from "@/lib/cn";

type BrandMarkProps = {
  className?: string;
};

export function BrandMark({ className }: BrandMarkProps) {
  return (
    <span
      aria-hidden="true"
      className={cn("relative inline-flex size-8 items-center justify-center rounded-lg bg-foreground", className)}
    >
      <span className="h-3.5 w-1 rounded-full bg-background" />
      <span className="ml-1 h-5 w-1 rounded-full bg-primary" />
      <span className="ml-1 h-2.5 w-1 rounded-full bg-background" />
    </span>
  );
}
