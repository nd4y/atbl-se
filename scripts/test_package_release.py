import hashlib,io,json,os,pathlib,subprocess,sys,tarfile,tempfile,unittest,zipfile

class PackageReceiptTest(unittest.TestCase):
 def fixture(self,change=None,optimize=False):
  temp=tempfile.TemporaryDirectory();self.addCleanup(temp.cleanup);root=pathlib.Path(temp.name)
  (root/'scripts').mkdir();(root/'build').mkdir()
  source=pathlib.Path(os.environ.get('ATBLSE_PACKAGER',str(pathlib.Path(__file__).with_name('package-release.py'))))
  (root/'scripts/package-release.py').write_bytes(source.read_bytes())
  files={'README.md':b'fixture','core/server.go':b'package core\nconst Version = "0.2.0-rc.3"\n','windows/Launcher.ps1':b"$v -eq '0.2.0-rc.3'",'deploy/compose.yaml':b'image: atblse:0.2.0-rc.3','go.mod':b'fixture','go.sum':b''}
  files.update({name:b'fixture notice' for name in ['README.ru.md','LICENSE.md','THIRD_PARTY_NOTICES.md','SECURITY.md','SECURITY.ru.md','licenses/go.txt','licenses/gorilla-websocket.txt']})
  sha=lambda b:hashlib.sha256(b).hexdigest()
  for name,data in files.items():p=root/name;p.parent.mkdir(exist_ok=True);p.write_bytes(data)
  (root/'source.json').write_text(json.dumps([dict(path=n,sha256=sha(b)) for n,b in files.items()]))
  names=['atblse-server','atblse-catalog-import','atblse-server.exe','atblse-catalog-import.exe','atblse.exe','ca-certificates.crt']
  binaries={}
  for name in names:
   data=('synthetic-not-executable:'+name).encode();(root/'build'/name).write_bytes(data);binaries[name]=dict(sha256=sha(data),bytes=len(data))
  receipt=dict(version='0.2.0-rc.3',sourceFiles={n:sha(b) for n,b in files.items() if n.startswith('core/') or n in ('go.mod','go.sum')},binaries=binaries)
  (root/'build/build-receipt.json').write_text(json.dumps(receipt));(root/'build/version.txt').write_text('0.2.0-rc.3');(root/'build/ca-certificates-copyright').write_text('fixture')
  config=b'{}';tag='atblse:0.2.0-rc.3'
  with tarfile.open(root/'build/image.tar','w') as tar:
   for name,data in [('config.json',config),('manifest.json',json.dumps([dict(Config='config.json',RepoTags=[tag])]).encode())]:
    info=tarfile.TarInfo(name);info.size=len(data);tar.addfile(info,io.BytesIO(data))
  image=dict(image='sha256:'+sha(config),tag=tag,archive_sha256=sha((root/'build/image.tar').read_bytes()),binary_sha256=binaries['atblse-server']['sha256'])
  (root/'build/image-manifest.json').write_text(json.dumps(image))
  if change:change(root,receipt)
  run=subprocess.run([sys.executable,*(['-O'] if optimize else []),str(root/'scripts/package-release.py'),'--build-dir',str(root/'build'),'--output-dir',str(root/'release'),'--source-manifest',str(root/'source.json')],capture_output=True,text=True)
  return root,run
 def test_valid(self):
  root,run=self.fixture();self.assertEqual(run.returncode,0,run.stderr);self.assertEqual(len(list((root/'release').glob('*.zip'))),3)
 def test_archives_keep_notices_and_both_languages(self):
  root,run=self.fixture();self.assertEqual(run.returncode,0,run.stderr)
  for artifact in (root/'release').glob('*.zip'):
   with zipfile.ZipFile(artifact) as archive:
    self.assertTrue({'README.md','README.ru.md','LICENSE.md','THIRD_PARTY_NOTICES.md','licenses/go.txt','licenses/gorilla-websocket.txt'} <= set(archive.namelist()))
 def test_optimized_python_still_rejects_stale_binary(self):
  root,run=self.fixture(lambda r,m:(r/'build/atblse.exe').write_bytes(b'previous-version'),optimize=True)
  self.assertNotEqual(run.returncode,0);self.assertFalse((root/'release').exists())
 def test_missing_translated_docs_rejected(self):
  def change(root,receipt):
   p=root/'source.json';p.write_text(json.dumps([x for x in json.loads(p.read_text()) if x['path']!='README.ru.md']))
  root,run=self.fixture(change);self.assertNotEqual(run.returncode,0);self.assertFalse((root/'release').exists())
 def test_stale_windows_binaries(self):
  for name in ('atblse.exe','atblse-server.exe','atblse-catalog-import.exe'):
   with self.subTest(name=name):
    root,run=self.fixture(lambda r,m:(r/'build'/name).write_bytes(b'previous-version'))
    self.assertNotEqual(run.returncode,0,'stale executable accepted');self.assertFalse((root/'release').exists())
 def test_different_runtime_source(self):
  def change(root,receipt):
   receipt['sourceFiles']['core/server.go']='0'*64;(root/'build/build-receipt.json').write_text(json.dumps(receipt))
  root,run=self.fixture(change);self.assertNotEqual(run.returncode,0,'unrelated build accepted');self.assertFalse((root/'release').exists())
 def test_missing_receipt(self):
  root,run=self.fixture(lambda r,m:(r/'build/build-receipt.json').unlink())
  self.assertNotEqual(run.returncode,0,'unproven build accepted');self.assertFalse((root/'release').exists())

if __name__=='__main__':unittest.main()
