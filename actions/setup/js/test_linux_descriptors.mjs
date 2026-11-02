import fs from "fs";
import path from "path";

// Model Linux descriptor-relative lookup with real macOS file I/O. Renames
// follow pinned directories, including when their original names are replaced.
export function simulateLinuxDescriptors(vi) {
  if (process.platform !== "darwin") return;
  const originals = { openSync: fs.openSync, lstatSync: fs.lstatSync, readlinkSync: fs.readlinkSync, renameSync: fs.renameSync };
  const pinned = new Map();
  const resolve = filename => {
    if (typeof filename !== "string") return filename;
    const match = /^\/proc\/self\/fd\/(\d+)(?:\/([\s\S]*))?$/.exec(filename);
    return match ? path.join(pinned.get(Number(match[1])), match[2] || "") : filename;
  };
  const open = fs.openSync;
  fs.openSync = (filename, ...args) => {
    const resolved = resolve(filename);
    const fd = open(resolved, ...args);
    pinned.set(fd, resolved);
    return fd;
  };
  for (const method of ["lstatSync", "readlinkSync"]) {
    const original = fs[method];
    fs[method] = (filename, ...args) => original(resolve(filename), ...args);
  }
  const rename = fs.renameSync;
  fs.renameSync = (source, target) => {
    rename(source, target);
    for (const [fd, filename] of pinned) {
      if (filename === source || filename.startsWith(`${source}${path.sep}`)) pinned.set(fd, target + filename.slice(source.length));
    }
  };
  vi.stubGlobal("process", new Proxy(process, { get: (target, key) => (key === "platform" ? "linux" : Reflect.get(target, key)) }));
  return () => Object.assign(fs, originals);
}
