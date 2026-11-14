import { CircleUserRound } from "lucide-react";

import { WorkspacePlaceholder } from "@/components/workspace-placeholder";

export default function ProfilePage() {
  return (
    <WorkspacePlaceholder
      description="Identity, organization membership, and account settings will be managed through the selected identity provider and Tacit records."
      emptyDescription="Account controls will appear here after managed authentication is integrated."
      emptyTitle="Account settings are not connected"
      icon={CircleUserRound}
      title="Account"
    />
  );
}
