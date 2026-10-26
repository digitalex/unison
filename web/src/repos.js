// repoName is how a repository is known: the last part of its URL
// ("https://github.com/digitalex/unison.git" → "unison").
export function repoName(url) {
  return url.replace(/\/+$/, "").split("/").pop().replace(/\.git$/, "");
}
