import { Search } from "lucide-react";

import { WorkspacePlaceholder } from "@/components/workspace-placeholder";

export default function SearchPage() {
  return (
    <WorkspacePlaceholder
      description="Permission-aware search will open the exact meeting, transcript, highlight, or playlist position."
      emptyDescription="Search will become available after the product records and secure projection are connected."
      emptyTitle="Search is not connected"
      icon={Search}
      title="Search workspace"
    />
  );
}
