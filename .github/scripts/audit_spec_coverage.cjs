const fs = require("node:fs");
const path = require("node:path");

function isWithin(root, target) {
  const relative = path.relative(root, target);
  return relative === "" || (!relative.startsWith(`..${path.sep}`) && relative !== ".." && !path.isAbsolute(relative));
}

function isRepositoryFile(root, target) {
  if (!isWithin(root, target)) return false;
  try {
    return fs.statSync(target).isFile() && isWithin(root, fs.realpathSync(target));
  } catch (error) {
    if (error.code === "ENOENT" || error.code === "ENOTDIR") return false;
    throw error;
  }
}

function auditSpecCoverage(repositoryRoot) {
  const root = fs.realpathSync(repositoryRoot);
  const directory = fs.realpathSync(path.join(root, ".github", "aw"));
  if (!isWithin(root, directory)) throw new Error("Specification directory is outside the repository");
  const files = fs
    .readdirSync(directory, { withFileTypes: true })
    .filter(entry => entry.name.endsWith(".md") && (entry.isFile() || entry.isSymbolicLink()))
    .map(entry => entry.name)
    .sort();
  const brokenLinks = [];
  const missingDescriptions = [];
  let linksChecked = 0;

  for (const name of files) {
    const filename = path.join(directory, name);
    if (!isRepositoryFile(root, filename)) throw new Error(`Specification is not a repository file: ${name}`);
    const content = fs.readFileSync(filename, "utf8");
    const frontmatter = content.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);
    if (!frontmatter || !/^description:/m.test(frontmatter[1])) missingDescriptions.push(name);
    let fence;
    let fenceLength = 0;
    for (const [index, line] of content.split(/\r?\n/).entries()) {
      const marker = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
      if (marker) {
        if (!fence && (marker[1][0] !== "`" || !marker[2].includes("`"))) {
          fence = marker[1][0];
          fenceLength = marker[1].length;
          continue;
        } else if (fence && marker[1][0] === fence && marker[1].length >= fenceLength && /^\s*$/.test(marker[2])) {
          fence = undefined;
          continue;
        }
      }
      if (fence) continue;
      const text = line.replace(/(`+).*?\1/g, "");
      const links = text.matchAll(/\[[^\]\r\n]*\]\(\s*(<[^>\r\n]+>|[^\s)]+)(?:\s+["'][^)\r\n]*["'])?\s*\)/g);
      for (const match of links) {
        const destination = match[1].replace(/^<|>$/g, "");
        if (/^[a-z][a-z\d+.-]*:/i.test(destination) || destination.startsWith("/") || destination.startsWith("#")) continue;
        const localPath = destination.split(/[?#]/)[0];
        if (!localPath.endsWith(".md")) continue;
        linksChecked++;
        let target;
        try {
          target = decodeURIComponent(localPath);
        } catch (error) {
          if (!(error instanceof URIError)) throw error;
          brokenLinks.push({ file: name, line: index + 1, target: destination, reason: "invalid URI encoding" });
          continue;
        }
        if (!isRepositoryFile(root, path.resolve(directory, target))) {
          brokenLinks.push({ file: name, line: index + 1, target: destination, reason: "missing or outside repository" });
        }
      }
    }
  }

  return {
    files_scanned: files.length,
    links_checked: linksChecked,
    broken_links: brokenLinks,
    missing_descriptions: missingDescriptions,
  };
}

module.exports = { auditSpecCoverage };

if (require.main === module) {
  process.stdout.write(`${JSON.stringify(auditSpecCoverage(process.cwd()), null, 2)}\n`);
}
