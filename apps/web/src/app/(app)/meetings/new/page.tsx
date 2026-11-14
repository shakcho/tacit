import { Upload } from "lucide-react";

import { WorkspacePlaceholder } from "@/components/workspace-placeholder";

export default function NewMeetingPage() {
  return (
    <WorkspacePlaceholder
      description="Meeting ingestion will connect provider recordings and direct uploads to the same recording model."
      emptyDescription="The upload boundary will arrive with the recording lifecycle. This route already has its final place in the product shell."
      emptyTitle="Recording upload is not connected"
      icon={Upload}
      title="Add a meeting recording"
    />
  );
}
