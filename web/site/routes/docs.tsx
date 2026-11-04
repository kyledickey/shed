import { createFileRoute } from "@tanstack/react-router";
import { DocsLayout } from "../../src/features/docs/DocsLayout";

export const Route = createFileRoute("/docs")({
  // The site's top bar is 56px tall.
  component: () => <DocsLayout barHeight={60} />,
});
