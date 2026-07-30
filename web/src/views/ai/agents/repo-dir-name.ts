/** Mirrors internal/ai/service/workspace.go sanitizeBranchForDir. */
export function sanitizeBranchForDir(branch: string): string {
  let s = branch.trim();
  if (!s) s = "main";

  let out = "";
  let prevDash = false;
  for (const ch of s) {
    const code = ch.codePointAt(0)!;
    const ok =
      (code >= 97 && code <= 122) ||
      (code >= 65 && code <= 90) ||
      (code >= 48 && code <= 57) ||
      ch === "." ||
      ch === "_";
    if (ok) {
      out += ch;
      prevDash = false;
      continue;
    }
    if (!prevDash) {
      out += "-";
      prevDash = true;
    }
  }

  s = out.replace(/^-+|-+$/g, "");
  if (!s) s = "branch";

  const maxLen = 100;
  if (s.length > maxLen) {
    s = s.slice(0, maxLen).replace(/^-+|-+$/g, "");
    if (!s) s = "branch";
  }
  return s;
}

/** Mirrors internal/ai/service/workspace.go repoDirName. */
export function repoDirName(repositoryId: number, branch: string): string {
  return `repo-${repositoryId}-${sanitizeBranchForDir(branch)}`;
}

/** Workspace-relative path shown in the binding UI, e.g. ./repo-4-refactor. */
export function repoBindingPath(repositoryId: number, branch: string): string {
  return `./${repoDirName(repositoryId, branch)}`;
}
