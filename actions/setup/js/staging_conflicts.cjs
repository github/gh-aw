const fs = require("fs");
const path = require("path");

const READ_BUFFER_SIZE = 64 * 1024;
const NO_FOLLOW = fs.constants.O_NOFOLLOW || 0;

function readChunk(fd, buffer, length, position) {
  let bytesRead = 0;
  while (bytesRead < length) {
    const count = fs.readSync(fd, buffer, bytesRead, length - bytesRead, position + bytesRead);
    if (count === 0) break;
    bytesRead += count;
  }
  return bytesRead;
}

function filesHaveSameContents(sourcePath, destPath) {
  const sourceStat = fs.lstatSync(sourcePath);
  const destStat = fs.lstatSync(destPath);
  if (!sourceStat.isFile() || !destStat.isFile() || sourceStat.size !== destStat.size) {
    return false;
  }

  let sourceFd;
  let destFd;
  try {
    sourceFd = fs.openSync(sourcePath, fs.constants.O_RDONLY | NO_FOLLOW);
    destFd = fs.openSync(destPath, fs.constants.O_RDONLY | NO_FOLLOW);
    const sourceBuffer = Buffer.allocUnsafe(READ_BUFFER_SIZE);
    const destBuffer = Buffer.allocUnsafe(READ_BUFFER_SIZE);
    let position = 0;
    while (position < sourceStat.size) {
      const length = Math.min(READ_BUFFER_SIZE, sourceStat.size - position);
      const sourceBytes = readChunk(sourceFd, sourceBuffer, length, position);
      const destBytes = readChunk(destFd, destBuffer, length, position);
      if (sourceBytes !== length || destBytes !== length || !sourceBuffer.subarray(0, length).equals(destBuffer.subarray(0, length))) {
        return false;
      }
      position += length;
    }
    return true;
  } finally {
    if (sourceFd !== undefined) fs.closeSync(sourceFd);
    if (destFd !== undefined) fs.closeSync(destFd);
  }
}

function listFilesRecursively(directory, relativePath = "") {
  const files = [];
  let entries;
  try {
    entries = fs.readdirSync(directory, { withFileTypes: true });
  } catch (error) {
    throw new Error(`Failed to read directory ${directory}`, { cause: error });
  }
  for (const entry of entries) {
    const childPath = path.join(relativePath, entry.name);
    const fullPath = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...listFilesRecursively(fullPath, childPath));
    } else if (entry.isFile()) {
      files.push(childPath);
    }
  }
  return files.sort();
}

function directoriesHaveSameContents(sourceDir, destDir) {
  const sourceStat = fs.lstatSync(sourceDir);
  const destStat = fs.lstatSync(destDir);
  if (!sourceStat.isDirectory() || !destStat.isDirectory()) {
    return false;
  }

  const sourceFiles = listFilesRecursively(sourceDir);
  const destFiles = listFilesRecursively(destDir);
  if (sourceFiles.length !== destFiles.length || sourceFiles.some((file, index) => file !== destFiles[index])) {
    return false;
  }

  return sourceFiles.every(file => filesHaveSameContents(path.join(sourceDir, file), path.join(destDir, file)));
}

function copyFileExclusive(sourcePath, destPath) {
  try {
    fs.copyFileSync(sourcePath, destPath, fs.constants.COPYFILE_EXCL);
    return "copied";
  } catch (error) {
    if (!(error instanceof Error) || Reflect.get(error, "code") !== "EEXIST") throw error;
    return filesHaveSameContents(sourcePath, destPath) ? "identical" : "conflict";
  }
}

module.exports = {
  copyFileExclusive,
  directoriesHaveSameContents,
};
