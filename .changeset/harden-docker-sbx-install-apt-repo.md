---
"gh-aw": patch
---

Harden `sudo_docker_sbx_install.sh`: stop piping the remote `get.docker.com` convenience script into a root shell (`curl | sudo sh`). The Docker apt repository is now configured deterministically by fetching the GPG signing key to a file and writing a `signed-by` apt source line, matching the pattern already used by `install_gh_cli.sh`.
