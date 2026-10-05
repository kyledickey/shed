import { createFileRoute } from "@tanstack/react-router";
import { DocsLayout } from "../../src/features/docs/DocsLayout";

export const Route = createFileRoute("/docs")({
  component: DocsLayout,
});
