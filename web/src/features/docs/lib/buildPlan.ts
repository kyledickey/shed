const keyPattern = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** isBuildKey reports whether a variable name is accepted as a build secret id. */
export function isBuildKey(key: string): boolean {
  return keyPattern.test(key);
}

/**
 * secretMounts renders a Dockerfile RUN instruction that mounts the given
 * variable names as BuildKit secrets and exports them for one command.
 * Names that are not valid secret ids are left out.
 */
export function secretMounts(keys: string[], command: string): string {
  const valid = keys.filter(isBuildKey).sort();
  const lines = [
    ...valid.map((k) => `--mount=type=secret,id=${k}`),
    ...valid.map((k) => `${k}="$(cat /run/secrets/${k})"`),
    command,
  ];
  return "RUN " + lines.join(" \\\n    ");
}
