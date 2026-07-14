import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { CodeViewer } from "./CodeViewer";

describe("CodeViewer", () => {
  it("pretty-prints a valid JSON value", () => {
    render(<CodeViewer label="Request body" value={JSON.stringify({ a: 1, b: "two" })} />);

    // JSON.stringify(..., null, 2) puts each key on its own line — proof the
    // value was actually parsed and reformatted, not shown verbatim.
    expect(screen.getByText(/"a": 1/)).toBeInTheDocument();
    expect(screen.getByText(/"b": "two"/)).toBeInTheDocument();
  });

  it("falls back to the raw string when the value isn't valid JSON", () => {
    render(<CodeViewer label="Response body" value="not json at all" />);

    expect(screen.getByText("not json at all")).toBeInTheDocument();
  });

  it("shows an explicit 'Empty' placeholder for an empty value", () => {
    render(<CodeViewer label="Response body" value="" />);

    expect(screen.getByText("Empty")).toBeInTheDocument();
  });

  // CRITICAL a11y (DESIGN.md §9, Slice 3 AC1/AC6): the server's own
  // "[REDACTED]" marker must render as literal, unmistakable text — never
  // re-redacted, reformatted, or hidden — and the marker must be
  // distinguished by more than color alone.
  it("renders the server's literal [REDACTED] marker as text, verbatim, without re-redacting it", () => {
    const body = JSON.stringify({ accessToken: "[REDACTED]", tool: "outlook-list-messages" });
    render(<CodeViewer label="Request body" value={body} />);

    // The exact marker string is present in the DOM as real text.
    expect(screen.getByText("[REDACTED]")).toBeInTheDocument();
    // Nothing beside it was altered — the sibling field is untouched.
    expect(screen.getByText(/"tool": "outlook-list-messages"/)).toBeInTheDocument();
  });

  it("distinguishes the redaction marker by text weight, not color alone", () => {
    const body = JSON.stringify({ secret: "[REDACTED]" });
    render(<CodeViewer label="Request body" value={body} />);

    const marker = screen.getByText("[REDACTED]");
    // font-semibold is a weight/shape signal, independent of the tint class
    // sitting on the surrounding line — an operator relying on grayscale or
    // a screen reader still gets "this is different" from the text/weight
    // alone, not only from color.
    expect(marker).toHaveClass("font-semibold", { exact: false });
    expect(marker.tagName.toLowerCase()).toBe("span");
  });

  it("is collapsible: the payload is shown by default and hides after the header is clicked", () => {
    render(<CodeViewer label="Request body" value={JSON.stringify({ a: 1 })} />);
    expect(screen.getByText(/"a": 1/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Request body" }));

    expect(screen.queryByText(/"a": 1/)).not.toBeInTheDocument();
  });
});
