import { Skeleton } from "@/components/ui/skeleton";

export default function ProductLoading() {
  return (
    <div aria-busy="true" aria-label="Loading workspace" className="space-y-8">
      <div className="space-y-3 border-b border-border pb-8">
        <Skeleton className="h-4 w-28" />
        <Skeleton className="h-10 w-full max-w-md" />
        <Skeleton className="h-5 w-full max-w-xl" />
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        <Skeleton className="h-36" />
        <Skeleton className="h-36" />
        <Skeleton className="h-36" />
      </div>
      <Skeleton className="h-80" />
    </div>
  );
}
