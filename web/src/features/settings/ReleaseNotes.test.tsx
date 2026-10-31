import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vite-plus/test";
import { ReleaseNotes } from "./ReleaseNotes";

const render = (notes: string) => renderToStaticMarkup(<ReleaseNotes notes={notes} />);

describe("ReleaseNotes", () => {
  it("renders headings, bullets, bold, code, and links", () => {
    const html = render(
      [
        "Nightly channel and Markdown notes.",
        "",
        "### Features",
        "",
        "#### Updates",
        "",
        "- **update:** add a `nightly` channel ([#12](https://github.com/o/shed/pull/12))",
        "- second",
        "  wrapped",
      ].join("\n"),
    );
    expect(html).toContain("<p>Nightly channel and Markdown notes.</p>");
    expect(html).toContain("<h4>Features</h4>");
    expect(html).toContain("<h5>Updates</h5>");
    expect(html).toContain(
      '<li><strong>update:</strong> add a <code>nightly</code> channel (<a href="https://github.com/o/shed/pull/12" target="_blank" rel="noreferrer noopener">#12</a>)</li>',
    );
    expect(html).toContain("<li>second wrapped</li>");
  });

  it("links only http(s) URLs and escapes HTML", () => {
    const html = render("[x](javascript:alert(1)) <script>bad()</script>");
    expect(html).not.toContain("href");
    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;script&gt;");
  });
});
