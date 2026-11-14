import { PlaySquare } from "lucide-react";

import { WorkspacePlaceholder } from "@/components/workspace-placeholder";

export default function PlaylistsPage() {
  return (
    <WorkspacePlaceholder
      description="Playlists will arrange accessible highlights in a deliberate order for review and sharing."
      emptyDescription="Create and ordering behavior will arrive with the playlist product module."
      emptyTitle="No playlists to show"
      icon={PlaySquare}
      title="Playlists"
    />
  );
}
