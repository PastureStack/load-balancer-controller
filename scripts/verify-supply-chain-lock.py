"""Reject a lock manifest that disagrees with the actual build inputs."""
import csv
from pathlib import Path
import re


def verify(root):
    rows = {r['name']: r for r in csv.DictReader((root / 'supply-chain.lock').open(), delimiter='\t')}
    dockerfile = (root / 'package/haproxy/Dockerfile').read_text()
    builder = (root / 'Dockerfile.dapper').read_text()
    for name, prefix in (('haproxy', 'HAPROXY'), ('aws-lc', 'AWS_LC')):
        version = re.search(r'^ARG ' + prefix + r'_VERSION=(\S+)$', dockerfile, re.M)[1]
        sha = re.search(r'^ARG ' + prefix + r'_SHA256=([0-9a-f]{64})$', dockerfile, re.M)[1]
        row = rows[name]
        assert row['version'] == version, name + ' lock version drift'
        assert row['integrity'] == 'sha256:' + sha, name + ' lock checksum drift'
        assert version in row['locator'], name + ' source URL drift'
    go_version = re.search(r'^ARG GO_VERSION=(\S+)$', builder, re.M)[1]
    for arch in ('amd64', 'arm64'):
        row = rows['go-linux-' + arch]
        sha = re.search(r'^ARG GO_LINUX_' + arch.upper() + r'_SHA256=(\S+)$', builder, re.M)[1]
        assert row['version'] == go_version and row['integrity'] == 'sha256:' + sha, 'Go lock drift: ' + arch
    scanner = rows['trivy']
    reference = 'ghcr.io/aquasecurity/trivy:' + scanner['version'] + '@' + scanner['integrity']
    for workflow in ('release.yml', 'security-release-gate.yml'):
        assert 'TRIVY_IMAGE: ' + reference in (root / '.github/workflows' / workflow).read_text(), 'Scanner lock drift'
    sums = set((root / 'go.sum').read_text().splitlines())
    for module, version in re.findall(r'^\s*((?:github\.com|golang\.org)/\S+)\s+(v\S+)', (root / 'go.mod').read_text(), re.M):
        row = rows[module]
        assert row['version'] == version and module + ' ' + version + ' ' + row['integrity'] in sums, 'Module lock drift: ' + module


if __name__ == '__main__':
    verify(Path(__file__).resolve().parents[1])
    print('SUPPLY_CHAIN_LOCK_MATCHES_BUILD_INPUTS')
