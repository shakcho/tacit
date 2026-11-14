import type { LucideIcon } from "lucide-react";

import { EmptyState } from "@/components/ui/empty-state";
import { PageHeader } from "@/components/ui/page-header";

type WorkspacePlaceholderProps = {
  description: string;
  emptyDescription: string;
  emptyTitle: string;
  icon: LucideIcon;
  title: string;
};

export function WorkspacePlaceholder({
  description,
  emptyDescription,
  emptyTitle,
  icon,
  title,
}: WorkspacePlaceholderProps) {
  return (
    <div className="space-y-8" data-enter>
      <PageHeader description={description} eyebrow="Tacit workspace" title={title} />
      <EmptyState description={emptyDescription} icon={icon} title={emptyTitle} />
    </div>
  );
}
