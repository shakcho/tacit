import { Inbox } from "lucide-react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { EmptyState } from "@/components/ui/empty-state";

describe("EmptyState", () => {
  it("announces a direct heading and supporting description", () => {
    render(
      <EmptyState
        description="Connect a meeting source or upload a recording."
        icon={Inbox}
        title="No meetings yet"
      />,
    );

    expect(screen.getByRole("heading", { level: 2, name: "No meetings yet" })).toBeDefined();
    expect(screen.getByText("Connect a meeting source or upload a recording.")).toBeDefined();
  });
});
