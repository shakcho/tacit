import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Button } from "@/components/ui/button";

describe("Button", () => {
  it("uses safe button semantics by default", () => {
    render(<Button>Upload a recording</Button>);

    const button = screen.getByRole("button", { name: "Upload a recording" });
    expect(button.getAttribute("type")).toBe("button");
  });

  it("reports the user action", () => {
    const onClick = vi.fn();
    render(<Button onClick={onClick}>Try again</Button>);

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(onClick).toHaveBeenCalledOnce();
  });
});
