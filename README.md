# dnsdist-cert-sync (Standalone)

This directory is self-contained and can be built/deployed independently from CoreDNS and subnet-manager.

## Quick start (in this directory)

```bash
make build
sudo make install
sudo make start
```

## Service files

- Binary: `/usr/local/bin/dnsdist-cert-sync`
- Config: `/etc/dnsdist-cert-sync/config.yaml`
- Env: `/etc/dnsdist-cert-sync/env`
- Systemd: `/etc/systemd/system/dnsdist-cert-sync.service`

## Standalone package

Create a standalone tarball from this directory:

```bash
make package
```

It outputs `dnsdist-cert-sync-standalone.tar.gz`.

## Sparse clone only this directory

If your source repo still contains other components, you can fetch only this folder:

```bash
git clone --filter=blob:none --no-checkout <repo-url>
cd <repo-dir>
git sparse-checkout init --cone
git sparse-checkout set dnsdist-cert-sync
git checkout <branch>
cd dnsdist-cert-sync
```
