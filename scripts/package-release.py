"""Package verified prebuilt binaries from an explicit source manifest; no build/network."""
import argparse
import datetime
import hashlib
import json
import pathlib
import re
import tarfile
import zipfile

p = argparse.ArgumentParser()
p.add_argument('--build-dir', type=pathlib.Path, required=True)
p.add_argument('--output-dir', type=pathlib.Path, required=True)
p.add_argument('--source-manifest', type=pathlib.Path, required=True)
args = p.parse_args()
def require(condition, message='Invalid or unverified release input'):
    if not condition:
        raise ValueError(message)
root = pathlib.Path(__file__).resolve().parents[1]
sha = lambda data: hashlib.sha256(data).hexdigest()
manifest = json.loads(args.source_manifest.read_text(encoding='utf-8'))
sources = {}
for item in manifest:
    name = item['path']
    path = pathlib.PurePosixPath(name)
    if not path.parts or '\\' in name or ':' in name or path.is_absolute() or '..' in path.parts or any(part in ('node_modules','out','dist','data','private','captures','firmware','__pycache__') for part in path.parts) or path.parts[0] not in ('.gitattributes','.gitignore','.repo-policy.json','.github','.standards','AGENTS.md','README.md','README.ru.md','CHANGELOG.md','CONTRIBUTING.md','CONTRIBUTING.ru.md','LICENSE.md','SECURITY.md','SECURITY.ru.md','THIRD_PARTY_NOTICES.md','go.mod','go.sum','core','internal','cmd','scripts','tests','reference','research','windows','deploy','docs','licenses'):
        raise ValueError('Unapproved source member: ' + name)
    source = root / name
    require(not any(part.is_symlink() for part in [source, *source.parents] if part != root.parent), 'Source link not allowed: ' + name)
    data = source.read_bytes()
    require(sha(data) == item['sha256'], 'Source drift: ' + name)
    require(name not in sources, 'Release validation failed')
    sources[name] = data
required_docs={'README.md','README.ru.md','LICENSE.md','THIRD_PARTY_NOTICES.md','SECURITY.md','SECURITY.ru.md','licenses/go.txt','licenses/gorilla-websocket.txt'}
require(required_docs <= sources.keys(), 'Bilingual documentation and license notices are required')
version = re.search(r'const Version = "([^"]+)"', sources['core/server.go'].decode()).group(1)
require(args.build_dir.joinpath('version.txt').read_text().strip() == version, 'Release validation failed')
require("-eq '" + version + "'" in sources['windows/Launcher.ps1'].decode('utf-8-sig'), 'Release validation failed')
require('atblse:' + version in sources['deploy/compose.yaml'].decode(), 'Release validation failed')
receipt = json.loads((args.build_dir / 'build-receipt.json').read_text())
require(receipt['version'] == version, 'Build receipt version mismatch')
required = {'atblse-server','atblse-catalog-import','atblse-server.exe','atblse-catalog-import.exe','atblse.exe','ca-certificates.crt'}
require(set(receipt['binaries']) == required, 'Incomplete build receipt')
runtime = {name for name in sources if (name in ('go.mod','go.sum') or name.startswith(('core/','internal/','cmd/'))) and not name.endswith('_test.go')}
require(set(receipt['sourceFiles']) == runtime, 'Incomplete runtime source receipt')
require(all((sha(sources[name]) == digest for name, digest in receipt['sourceFiles'].items())), 'Binaries were built from different sources')
for name,record in receipt['binaries'].items():
    data = (args.build_dir/name).read_bytes()
    require(sha(data) == record['sha256'] and len(data) == record['bytes'], 'Stale or changed build output: ' + name)
image = json.loads((args.build_dir / 'image-manifest.json').read_text())
image_bytes = (args.build_dir / 'image.tar').read_bytes()
require(image['archive_sha256'] == sha(image_bytes), 'Release validation failed')
require(image['binary_sha256'] == sha((args.build_dir / 'atblse-server').read_bytes()), 'Release validation failed')
with tarfile.open(args.build_dir / 'image.tar') as archive:
    images = json.load(archive.extractfile('manifest.json'))
    require(len(images) == 1 and image['tag'] in images[0]['RepoTags'], 'Release validation failed')
    require('sha256:' + sha(archive.extractfile(images[0]['Config']).read()) == image['image'], 'Release validation failed')
require(not args.output_dir.exists(), 'Preserve existing release directory')
args.output_dir.mkdir(parents=True)
base = dict(version=version, candidate=True, physical_device_accepted=False,
            source_manifest_sha256=sha(args.source_manifest.read_bytes()),
            source_identity='exact reviewed source file manifest; built runtime hashes verified',
            created_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),
            docker=image, build_receipt_sha256=sha((args.build_dir/'build-receipt.json').read_bytes()))
artifacts = {}
def package(kind, members):
    details = dict(base, files_sha256={name:sha(data) for name,data in sorted(members.items())})
    members = dict(members, **{'release-manifest.json':(json.dumps(details,indent=2)+'\n').encode()})
    target = args.output_dir / ('atblse-' + kind + '-' + version + '.zip')
    with zipfile.ZipFile(target,'x',zipfile.ZIP_DEFLATED) as archive:
        for name,data in sorted(members.items()):
            archive.writestr(name,data)
    with zipfile.ZipFile(target) as archive:
        require(archive.testzip() is None and set(archive.namelist()) == set(members), 'Release validation failed')
        require(all((sha(archive.read(name)) == sha(data) for name, data in members.items())), 'Release validation failed')
    artifacts[target.name] = dict(sha256=sha(target.read_bytes()),bytes=target.stat().st_size)
common={k:v for k,v in sources.items() if k in required_docs or k=='CHANGELOG.md' or k.startswith(('docs/','licenses/'))}
win=dict(common,**{k:v for k,v in sources.items() if k.startswith('windows/')})
for name in ('atblse.exe','atblse-server.exe','atblse-catalog-import.exe'): win[name]=(args.build_dir/name).read_bytes()
package('windows',win)
nas=dict(common,**{k:v for k,v in sources.items() if k.startswith('deploy/')})
for name in ('image.tar','atblse-catalog-import','ca-certificates-copyright'):nas[name]=(args.build_dir/name).read_bytes()
package('nas',nas)
package('source',dict(sources,**{'source-manifest.json':args.source_manifest.read_bytes()}))
base['artifacts']=artifacts
(args.output_dir/'release-manifest.json').write_text(json.dumps(base,indent=2)+'\n',encoding='utf-8')
print(json.dumps(base,indent=2))
