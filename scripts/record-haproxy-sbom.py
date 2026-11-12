"""Record the source-built HAProxy and its statically linked AWS-LC."""
import json
from pathlib import Path
import re
import subprocess
import sys
from tempfile import TemporaryDirectory

image, filename = sys.argv[1:]
labels = json.loads(subprocess.check_output(['docker', 'image', 'inspect', '-f', '{{json .Config.Labels}}', image]))
path = Path(filename)
bom = json.loads(path.read_text(encoding='utf-8'))
root = bom['metadata']['component']['bom-ref']
components = bom['components']
dependencies = bom['dependencies']
references = []
for name, kind, purl, source in (
    ('haproxy', 'application', 'pkg:generic/haproxy', 'https://www.haproxy.org/download/3.4/src/haproxy-{version}.tar.gz'),
    ('aws-lc', 'library', 'pkg:github/aws/aws-lc', 'https://codeload.github.com/aws/aws-lc/tar.gz/refs/tags/v{version}'),
):
    version = labels[f'org.pasturestack.{name}.version']
    digest = labels[f'org.pasturestack.{name}.source-sha256']
    assert re.fullmatch(r'\d+\.\d+\.\d+', version) and re.fullmatch(r'[0-9a-f]{64}', digest)
    reference = f'{purl}@{version}'
    references.append(reference)
    assert all(component.get('bom-ref') != reference for component in components)
    components.append({'type': kind, 'name': name, 'version': version, 'purl': reference,
                       'bom-ref': reference, 'externalReferences': [{
                           'type': 'distribution', 'url': source.format(version=version),
                           'hashes': [{'alg': 'SHA-256', 'content': digest}],
                       }]})
haproxy, aws_lc = references
root_dependency = next((entry for entry in dependencies if entry['ref'] == root), None)
if root_dependency is None:
    root_dependency = {'ref': root, 'dependsOn': []}
    dependencies.append(root_dependency)
root_dependency.setdefault('dependsOn', []).append(haproxy)
dependencies.extend([{'ref': haproxy, 'dependsOn': [aws_lc]}, {'ref': aws_lc, 'dependsOn': []}])
# Trivy runs in Docker and owns the file. The runner owns its parent directory.
with TemporaryDirectory(dir=path.parent) as workdir:
    updated = Path(workdir) / path.name
    updated.write_text(json.dumps(bom, indent=2, ensure_ascii=False) + '\n', encoding='utf-8')
    updated.replace(path)
