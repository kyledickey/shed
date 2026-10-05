/** Converts arbitrary text to a DNS label usable as a service name. */
export function toServiceName(input: string): string {
  return input
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 63)
    .replace(/-+$/, "");
}

export function repoName(fullName: string): string {
  return fullName.split("/").pop() ?? fullName;
}

export function imageName(ref: string): string {
  const path = ref.split("@")[0]?.split("/").pop() ?? "";
  return path.split(":")[0] ?? "";
}

export function shortSha(sha: string): string {
  return sha.slice(0, 7);
}
