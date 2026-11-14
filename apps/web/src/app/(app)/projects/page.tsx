import { FolderKanban } from "lucide-react";

import { WorkspacePlaceholder } from "@/components/workspace-placeholder";

export default function ProjectsPage() {
  return (
    <WorkspacePlaceholder
      description="Projects will group accessible meetings, highlights, and playlists inside an organization."
      emptyDescription="Project records will appear here after organization and membership work is connected."
      emptyTitle="No projects to show"
      icon={FolderKanban}
      title="Projects"
    />
  );
}
