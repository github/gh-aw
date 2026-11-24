const fs = require("fs");
const path = require("path");
const { withGuardedDirectory } = require("./symlink_guard.cjs");

const READ_BUFFER_SIZE = 64 * 1024;
const FILE_OPEN_FLAGS = fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK;
const DIRECTORY_OPEN_FLAGS = fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_DIRECTORY;

function sameMetadata(first, second) {
  return first.dev === second.dev && first.ino === second.ino && first.size === second.size && first.mtimeMs === second.mtimeMs && first.ctimeMs === second.ctimeMs;
}

function assertStableFile(directory, name, fd, expectedStat) {
  const openedStat = fs.fstatSync(fd);
  const currentStat = fs.lstatSync(path.join(directory, name));
  if (!openedStat.isFile() || !sameMetadata(expectedStat, openedStat) || !sameMetadata(openedStat, currentStat)) {
    throw new Error(`File changed while comparing ${path.join(directory, name)}`);
  }
}

function readChunk(fd, buffer, length, position) {
  let bytesRead = 0;
  while (bytesRead < length) {
    const count = fs.readSync(fd, buffer, bytesRead, length - bytesRead, position + bytesRead);
    if (count === 0) break;
    bytesRead += count;
  }
  return bytesRead;
}

function filesHaveSameContentsAt(sourceDirectory, sourceName, destDirectory, destName) {
  const sourcePath = path.join(sourceDirectory, sourceName);
  const destPath = path.join(destDirectory, destName);
  const sourceStat = fs.lstatSync(sourcePath);
  const destStat = fs.lstatSync(destPath);
  if (!sourceStat.isFile() || !destStat.isFile() || sourceStat.size !== destStat.size) {
    return false;
  }

  let sourceFd;
  let destFd;
  try {
    sourceFd = fs.openSync(sourcePath, FILE_OPEN_FLAGS);
    destFd = fs.openSync(destPath, FILE_OPEN_FLAGS);
    assertStableFile(sourceDirectory, sourceName, sourceFd, sourceStat);
    assertStableFile(destDirectory, destName, destFd, destStat);
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
    assertStableFile(sourceDirectory, sourceName, sourceFd, sourceStat);
    assertStableFile(destDirectory, destName, destFd, destStat);
    return true;
  } finally {
    if (sourceFd !== undefined) fs.closeSync(sourceFd);
    if (destFd !== undefined) fs.closeSync(destFd);
  }
}

function filesHaveSameContents(sourcePath, destPath) {
  return withGuardedDirectory(path.dirname(sourcePath), false, sourceDirectory =>
    withGuardedDirectory(path.dirname(destPath), false, destDirectory => filesHaveSameContentsAt(sourceDirectory, path.basename(sourcePath), destDirectory, path.basename(destPath)))
  );
}

function openPinnedDirectory(parentDirectory, name) {
  const directoryPath = path.join(parentDirectory, name);
  const expectedStat = fs.lstatSync(directoryPath);
  if (!expectedStat.isDirectory()) {
    throw new Error(`Directory changed while comparing ${directoryPath}`);
  }
  const fd = fs.openSync(directoryPath, DIRECTORY_OPEN_FLAGS);
  try {
    const openedStat = fs.fstatSync(fd);
    if (!openedStat.isDirectory() || !sameMetadata(expectedStat, openedStat)) {
      throw new Error(`Directory changed while opening ${directoryPath}`);
    }
    return { fd, path: `/proc/self/fd/${fd}`, parentDirectory, name, expectedStat };
  } catch (error) {
    fs.closeSync(fd);
    throw error;
  }
}

function assertStableDirectory(directory) {
  const openedStat = statDirectory(directory.path);
  const currentStat = fs.lstatSync(path.join(directory.parentDirectory, directory.name));
  if (!openedStat.isDirectory() || !sameMetadata(directory.expectedStat, openedStat) || !sameMetadata(openedStat, currentStat)) {
    throw new Error(`Directory changed while comparing ${path.join(directory.parentDirectory, directory.name)}`);
  }
}

function statDirectory(directory) {
  try {
    return fs.statSync(directory);
  } catch (error) {
    throw new Error(`Failed to stat directory ${directory}`, { cause: error });
  }
}

function readDirectoryEntries(directory) {
  try {
    return fs.readdirSync(directory, { withFileTypes: true });
  } catch (error) {
    throw new Error(`Failed to read directory ${directory}`, { cause: error });
  }
}

function directoriesAtHaveSameContents(sourceDir, destDir) {
  const sourceStat = statDirectory(sourceDir);
  const destStat = statDirectory(destDir);
  const sourceEntries = readDirectoryEntries(sourceDir).filter(entry => entry.isDirectory() || entry.isFile());
  const destEntries = readDirectoryEntries(destDir).filter(entry => entry.isDirectory() || entry.isFile());
  sourceEntries.sort((first, second) => first.name.localeCompare(second.name));
  destEntries.sort((first, second) => first.name.localeCompare(second.name));
  if (sourceEntries.length !== destEntries.length || sourceEntries.some((entry, index) => entry.name !== destEntries[index].name || entry.isDirectory() !== destEntries[index].isDirectory())) {
    return false;
  }

  for (let index = 0; index < sourceEntries.length; index++) {
    const sourceEntry = sourceEntries[index];
    const destEntry = destEntries[index];
    if (sourceEntry.isDirectory()) {
      const sourceChild = openPinnedDirectory(sourceDir, sourceEntry.name);
      let destChild;
      try {
        destChild = openPinnedDirectory(destDir, destEntry.name);
        if (!directoriesAtHaveSameContents(sourceChild.path, destChild.path)) return false;
        assertStableDirectory(sourceChild);
        assertStableDirectory(destChild);
      } finally {
        fs.closeSync(sourceChild.fd);
        if (destChild) fs.closeSync(destChild.fd);
      }
    } else if (!filesHaveSameContentsAt(sourceDir, sourceEntry.name, destDir, destEntry.name)) {
      return false;
    }
  }

  if (!sameMetadata(sourceStat, statDirectory(sourceDir))) {
    throw new Error(`Directory changed while comparing ${sourceDir}`);
  }
  if (!sameMetadata(destStat, statDirectory(destDir))) {
    throw new Error(`Directory changed while comparing ${destDir}`);
  }
  return true;
}

function directoriesHaveSameContents(sourceDir, destDir) {
  return withGuardedDirectory(sourceDir, false, sourceRoot => withGuardedDirectory(destDir, false, destRoot => directoriesAtHaveSameContents(sourceRoot, destRoot)));
}

function existingDirectoryStageState(sourceDir, destDir) {
  let destinationStat;
  try {
    destinationStat = fs.lstatSync(destDir);
  } catch (error) {
    const code = error && typeof error === "object" ? Reflect.get(error, "code") : undefined;
    if (code === "ENOENT") return null;
    throw new Error(`Failed to inspect staging directory ${destDir}`, { cause: error });
  }
  if (!destinationStat.isDirectory()) return "conflict";
  return directoriesHaveSameContents(sourceDir, destDir) ? "identical" : "conflict";
}

function copyDirectoryExclusive(sourceDir, destDir, copyContents) {
  const existingState = existingDirectoryStageState(sourceDir, destDir);
  if (existingState !== null) return existingState;

  const parentDir = path.dirname(destDir);
  try {
    fs.mkdirSync(parentDir, { recursive: true });
  } catch (error) {
    throw new Error(`Failed to create staging parent directory ${parentDir}`, { cause: error });
  }
  let tempDir;
  try {
    tempDir = fs.mkdtempSync(path.join(parentDir, `.${path.basename(destDir)}.stage-`));
  } catch (error) {
    throw new Error(`Failed to create temporary staging directory for ${destDir}`, { cause: error });
  }
  try {
    copyContents(tempDir);
    const currentState = existingDirectoryStageState(sourceDir, destDir);
    if (currentState !== null) return currentState;
    try {
      fs.renameSync(tempDir, destDir);
      return "copied";
    } catch (error) {
      const code = error && typeof error === "object" ? Reflect.get(error, "code") : undefined;
      if (code === "EEXIST" || code === "ENOTEMPTY") {
        const currentState = existingDirectoryStageState(sourceDir, destDir);
        if (currentState !== null) return currentState;
      }
      throw new Error(`Failed to publish staging directory ${destDir}`, { cause: error });
    }
  } finally {
    try {
      fs.rmSync(tempDir, { recursive: true, force: true });
    } catch (error) {
      throw new Error(`Failed to clean up temporary staging directory ${tempDir}`, { cause: error });
    }
  }
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
  copyDirectoryExclusive,
  directoriesHaveSameContents,
};
