#!/usr/bin/python3 -I
import hashlib
import os
import re
import stat
import subprocess
import sys


def fail(message):
    raise SystemExit(message)


def read_regular(path, limit):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or not 0 < info.st_size <= limit:
            fail("Unsafe helper installer input")
        data = source.read(limit + 1)
    if len(data) != info.st_size:
        fail("Invalid helper installer input size")
    return data


def trusted_directory(fd):
    info = os.fstat(fd)
    if info.st_uid != 0 or info.st_mode & 0o022:
        fail("Unsafe helper installation directory")


def open_directory(path, create=False):
    fd = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        trusted_directory(fd)
        for component in path.strip("/").split("/"):
            if create:
                try:
                    os.mkdir(component, 0o755, dir_fd=fd)
                except FileExistsError:
                    pass
            child = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            trusted_directory(child)
            os.close(fd)
            fd = child
        return fd
    except BaseException:
        os.close(fd)
        raise


def publish(directory_fd, name, data, mode):
    staging = name + ".new-" + os.urandom(16).hex()
    try:
        fd = os.open(staging, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode, dir_fd=directory_fd)
        try:
            os.fchown(fd, 0, 0)
            os.fchmod(fd, mode)
            if os.write(fd, data) != len(data):
                fail("Incomplete helper installation")
            os.fsync(fd)
        finally:
            os.close(fd)
        os.replace(staging, name, src_dir_fd=directory_fd, dst_dir_fd=directory_fd)
        os.fsync(directory_fd)
    finally:
        try:
            os.unlink(staging, dir_fd=directory_fd)
        except FileNotFoundError:
            pass


def install_sudoers(uid):
    directory = "/etc/sudoers.d"
    directory_fd = open_directory(directory)
    name = "awf-enclave-storage-" + uid
    staging = name + ".pending-" + os.urandom(8).hex()
    policy = (f"#{uid} ALL=(root) NOPASSWD: /usr/bin/python3 -I "
              "/var/lib/awf/enclave-storage-helper.py *\n").encode()
    try:
        fd = os.open(staging, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o440, dir_fd=directory_fd)
        try:
            os.fchown(fd, 0, 0)
            os.fchmod(fd, 0o440)
            if os.write(fd, policy) != len(policy):
                fail("Incomplete sudoers policy")
            os.fsync(fd)
        finally:
            os.close(fd)
        subprocess.run(["/usr/sbin/visudo", "-cf", directory + "/" + staging],
                       env={"PATH": "/usr/bin:/bin", "LANG": "C"}, check=True)
        os.replace(staging, name, src_dir_fd=directory_fd, dst_dir_fd=directory_fd)
        os.fsync(directory_fd)
    finally:
        try:
            os.unlink(staging, dir_fd=directory_fd)
        except FileNotFoundError:
            pass
        os.close(directory_fd)


def main(args):
    if len(args) != 6 or os.getuid() != 0 or os.geteuid() != 0:
        fail("Invalid helper installer invocation")
    manifest_path, helper_path, manifest_sha256, helper_sha256, version, uid = args
    if not re.fullmatch(r"[0-9a-f]{64}", manifest_sha256) or not re.fullmatch(r"[0-9a-f]{64}", helper_sha256):
        fail("Invalid reviewed helper digest")
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        fail("Invalid helper version")
    if not re.fullmatch(r"[1-9][0-9]{0,9}", uid) or int(uid) > 2147483647:
        fail("Invalid installing user")
    manifest = read_regular(manifest_path, 1024 * 1024)
    if hashlib.sha256(manifest).hexdigest() != manifest_sha256:
        fail("Reviewed checksum manifest changed")
    helper = read_regular(helper_path, 16 * 1024 * 1024)
    if hashlib.sha256(helper).hexdigest() != helper_sha256:
        fail("Helper checksum mismatch")
    entries = [line.split() for line in manifest.decode("ascii").splitlines()
               if len(line.split()) == 2 and line.split()[1] == "enclave-storage-helper.py"]
    if len(entries) != 1 or entries[0][0] != helper_sha256:
        fail("Helper checksum is not bound to the reviewed manifest")
    if (b'VERSION = "' + version.encode() + b'"') not in helper:
        fail("Helper version mismatch")
    directory_fd = open_directory("/var/lib/awf", create=True)
    try:
        os.fchmod(directory_fd, 0o755)
        publish(directory_fd, "enclave-storage-helper.py", helper, 0o644)
    finally:
        os.close(directory_fd)
    subprocess.run(["/usr/bin/python3", "-I", "/var/lib/awf/enclave-storage-helper.py", "install"],
                   env={"PATH": "/usr/bin:/bin", "LANG": "C"}, check=True)
    install_sudoers(uid)


if __name__ == "__main__":
    main(sys.argv[1:])
