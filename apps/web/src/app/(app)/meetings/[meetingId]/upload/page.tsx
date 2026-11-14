import { Upload } from "lucide-react";

import { WorkspacePlaceholder } from "@/components/workspace-placeholder";

export default function MeetingUploadPage() {
  return (
    <WorkspacePlaceholder
      description="This compatibility destination will attach an upload to an existing meeting after recording lifecycle work lands."
      emptyDescription="No upload has started. The legacy address now resolves to this stable App Router destination."
      emptyTitle="Meeting upload is ready for integration"
      icon={Upload}
      title="Upload meeting media"
    />
  );
}
