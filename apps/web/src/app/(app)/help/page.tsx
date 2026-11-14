import { CircleHelp } from "lucide-react";

import { WorkspacePlaceholder } from "@/components/workspace-placeholder";

export default function HelpPage() {
  return (
    <WorkspacePlaceholder
      description="Product guidance will follow the same meeting, transcript, highlight, and playlist language used in the workspace."
      emptyDescription="Help content will be written as product workflows become available."
      emptyTitle="Help content is being established"
      icon={CircleHelp}
      title="Help"
    />
  );
}
