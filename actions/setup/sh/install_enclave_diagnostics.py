#!/usr/bin/python3 -I
import hashlib
import io
import os
import stat
import sys
import tarfile


def reject(category):
    sys.exit("::error::Trusted enclave collector rejected: " + category)


def protected_directory(path):
    info = os.lstat(path)
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
        reject("unsafe-directory")


def read_regular(path, limit):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            reject("unsafe-file")
        if not 0 < info.st_size <= limit:
            reject("file-size")
        data = source.read(limit + 1)
    if len(data) != info.st_size:
        reject("file-size")
    return data


def install_verified(path, data, mode):
    if os.path.lexists(path):
        info = os.lstat(path)
        if info.st_uid != 0 or stat.S_IMODE(info.st_mode) != mode:
            reject("unsafe-installed-file")
        if read_regular(path, len(data)) != data:
            reject("installed-checksum")
        return
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, mode)
    with os.fdopen(fd, "wb") as destination:
        destination.write(data)
        os.fchmod(destination.fileno(), mode)


def main(args):
    try:
        pins = {
            "x86_64": ("x64", "6c172a9ab6718f7b801fb5a7bd486af2ee5e32758086a04b84fcce2aa77095e6"),
            "aarch64": ("arm64", "dbabf7a1cbf947e6059ffad2c27b80041db97dea1e553b62afdee7f13fca336e"),
        }
        if os.getuid() != 0 or os.geteuid() != 0:
            reject("bootstrap-must-run-root")
        if len(args) != 2 or os.uname().machine not in pins:
            reject("unsupported-architecture")
        arch, digest = pins[os.uname().machine]
        data = read_regular(args[0], 128 * 1024 * 1024)
        if hashlib.sha256(data).hexdigest() != digest:
            reject("archive-checksum")
        collector = read_regular(args[1], 1024 * 1024)
        if hashlib.sha256(collector).hexdigest() != "7722affc5c076d79692d4268a543ef267281424ad4b374eb8f80c24cbec08cae":
            reject("collector-checksum")
        with tarfile.open(fileobj=io.BytesIO(data), mode="r:xz") as archive:
            member = archive.getmember("node-v24.22.1-linux-" + arch + "/bin/node")
            if not member.isfile() or not member.mode & 0o111:
                reject("unsafe-executable")
            if not 0 < member.size <= 256 * 1024 * 1024:
                reject("executable-size")
            executable = archive.extractfile(member).read(member.size + 1)
            if len(executable) != member.size:
                reject("executable-size")
        root = "/var/lib/awf-enclave-smoke-export"
        for directory in ("/", "/var", "/var/lib"):
            protected_directory(directory)
        if not os.path.lexists(root):
            os.mkdir(root, 0o711)
            os.chmod(root, 0o711)
        protected_directory(root)
        install_verified(root + "/node", executable, 0o500)
        install_verified(root + "/collector.cjs", collector, 0o600)
    except OSError:
        reject("filesystem-check")
    except (tarfile.TarError, KeyError, EOFError):
        reject("archive-member")


if __name__ == "__main__":
    main(sys.argv[1:])
