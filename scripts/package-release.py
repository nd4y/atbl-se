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
root = pathlib.Path(__file__).resolve().parents[1]
sha = lambda data: hashlib.sha256(data).hexdigest()
manifest = json.loads(args.source_manifest.read_text(encoding='utf-8'))
sources = {}
for item in manifest:
    name = item['path']
    path = pathlib.PurePosixPath(name)
    if path.is_absolute() or '..' in path.parts or path.parts[0] not in ('.gitattributes','README.md','go.mod','go.sum','core','internal','cmd','scripts','research','windows','deploy','docs','licenses'):
        raise ValueError('Unapproved source member: ' + name)
    data = (root / name).read_bytes()
    assert sha(data) == item['sha256'], 'Source drift: ' + name
    assert name not in sources
    sources[name] = data
version = re.search(r'const Version = "([^"]+)"', sources['core/server.go'].decode()).group(1)
assert args.build_dir.joinpath('version.txt').read_text().strip() == version
assert "-eq '" + version + "'" in sources['windows/Launcher.ps1'].decode('utf-8-sig')
assert 'atbl-se:' + version in sources['deploy/compose.yaml'].decode()
receipt = json.loads((args.build_dir / 'build-receipt.json').read_text())
assert receipt['version'] == version, 'Build receipt version mismatch'
required = {'autoblow-server','autoblow-catalog-import','autoblow-server.exe','autoblow-catalog-import.exe','AutoblowLocal.exe','ca-certificates.crt'}
assert set(receipt['binaries']) == required, 'Incomplete build receipt'
runtime = {name for name in sources if (name in ('go.mod','go.sum') or name.startswith(('core/','internal/','cmd/'))) and not name.endswith('_test.go')}
assert set(receipt['sourceFiles']) == runtime, 'Incomplete runtime source receipt'
assert all(sha(sources[name]) == digest for name,digest in receipt['sourceFiles'].items()), 'Binaries were built from different sources'
for name,record in receipt['binaries'].items():
    data = (args.build_dir/name).read_bytes()
    assert sha(data) == record['sha256'] and len(data) == record['bytes'], 'Stale or changed build output: ' + name
image = json.loads((args.build_dir / 'image-manifest.json').read_text())
image_bytes = (args.build_dir / 'image.tar').read_bytes()
assert image['archive_sha256'] == sha(image_bytes)
assert image['binary_sha256'] == sha((args.build_dir / 'autoblow-server').read_bytes())
with tarfile.open(args.build_dir / 'image.tar') as archive:
    images = json.load(archive.extractfile('manifest.json'))
    assert len(images) == 1 and image['tag'] in images[0]['RepoTags']
    assert 'sha256:' + sha(archive.extractfile(images[0]['Config']).read()) == image['image']
assert not args.output_dir.exists(), 'Preserve existing release directory'
args.output_dir.mkdir(parents=True)
base = dict(version=version, candidate=True, physical_device_accepted=False,
            source_manifest_sha256=sha(args.source_manifest.read_bytes()),
            source_identity='exact file manifest; includes reviewed uncommitted development',
            created_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),
            docker=image, build_receipt_sha256=sha((args.build_dir/'build-receipt.json').read_bytes()))
artifacts = {}
def package(kind, members):
    details = dict(base, files_sha256={name:sha(data) for name,data in sorted(members.items())})
    members = dict(members, **{'release-manifest.json':(json.dumps(details,indent=2)+'\n').encode()})
    target = args.output_dir / ('atbl-se-' + kind + '-' + version + '.zip')
    with zipfile.ZipFile(target,'x',zipfile.ZIP_DEFLATED) as archive:
        for name,data in sorted(members.items()): archive.writestr(name,data)
    with zipfile.ZipFile(target) as archive:
        assert archive.testzip() is None and set(archive.namelist()) == set(members)
        assert all(sha(archive.read(name)) == sha(data) for name,data in members.items())
    artifacts[target.name] = dict(sha256=sha(target.read_bytes()),bytes=target.stat().st_size)
common={k:v for k,v in sources.items() if k=='README.md' or k.startswith(('docs/','licenses/'))}
win=dict(common,**{k:v for k,v in sources.items() if k.startswith('windows/')})
for name in ('AutoblowLocal.exe','autoblow-server.exe','autoblow-catalog-import.exe'): win[name]=(args.build_dir/name).read_bytes()
package('windows',win)
nas=dict(common,**{k:v for k,v in sources.items() if k.startswith('deploy/')})
for name in ('image.tar','autoblow-catalog-import','ca-certificates-copyright'):nas[name]=(args.build_dir/name).read_bytes()
package('nas',nas)
package('source',dict(sources,**{'source-manifest.json':args.source_manifest.read_bytes()}))
base['artifacts']=artifacts
(args.output_dir/'release-manifest.json').write_text(json.dumps(base,indent=2)+'\n',encoding='utf-8')
print(json.dumps(base,indent=2))
