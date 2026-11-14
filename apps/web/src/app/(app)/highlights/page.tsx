import { ListVideo } from "lucide-react";

import { WorkspacePlaceholder } from "@/components/workspace-placeholder";

export default function HighlightsPage() {
  return (
    <WorkspacePlaceholder
      description="Manual and accepted generated highlights will share one editable library."
      emptyDescription="Highlights will appear after meeting transcripts and manual highlight creation are connected."
      emptyTitle="No highlights to show"
      icon={ListVideo}
      title="Highlights"
    />
  );
}
