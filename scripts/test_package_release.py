import hashlib,io,json,os,pathlib,subprocess,sys,tarfile,tempfile,unittest

class PackageReceiptTest(unittest.TestCase):
 def fixture(self,change=None):
  temp=tempfile.TemporaryDirectory();self.addCleanup(temp.cleanup);root=pathlib.Path(temp.name)
  (root/'scripts').mkdir();(root/'build').mkdir()
  source=pathlib.Path(os.environ.get('AUTOBLOW_PACKAGER',str(pathlib.Path(__file__).with_name('package-release.py'))))
  (root/'scripts/package-release.py').write_bytes(source.read_bytes())
  files={'README.md':b'fixture','core/server.go':b'package core\nconst Version = "0.2.0-rc.2"\n','windows/Launcher.ps1':b"$v -eq '0.2.0-rc.2'",'deploy/compose.yaml':b'image: atbl-se:0.2.0-rc.2','go.mod':b'fixture','go.sum':b''}
  sha=lambda b:hashlib.sha256(b).hexdigest()
  for name,data in files.items():p=root/name;p.parent.mkdir(exist_ok=True);p.write_bytes(data)
  (root/'source.json').write_text(json.dumps([dict(path=n,sha256=sha(b)) for n,b in files.items()]))
  names=['autoblow-server','autoblow-catalog-import','autoblow-server.exe','autoblow-catalog-import.exe','AutoblowLocal.exe','ca-certificates.crt']
  binaries={}
  for name in names:
   data=('synthetic-not-executable:'+name).encode();(root/'build'/name).write_bytes(data);binaries[name]=dict(sha256=sha(data),bytes=len(data))
  receipt=dict(version='0.2.0-rc.2',sourceFiles={n:sha(b) for n,b in files.items() if n.startswith('core/') or n in ('go.mod','go.sum')},binaries=binaries)
  (root/'build/build-receipt.json').write_text(json.dumps(receipt));(root/'build/version.txt').write_text('0.2.0-rc.2');(root/'build/ca-certificates-copyright').write_text('fixture')
  config=b'{}';tag='atbl-se:0.2.0-rc.2'
  with tarfile.open(root/'build/image.tar','w') as tar:
   for name,data in [('config.json',config),('manifest.json',json.dumps([dict(Config='config.json',RepoTags=[tag])]).encode())]:
    info=tarfile.TarInfo(name);info.size=len(data);tar.addfile(info,io.BytesIO(data))
  image=dict(image='sha256:'+sha(config),tag=tag,archive_sha256=sha((root/'build/image.tar').read_bytes()),binary_sha256=binaries['autoblow-server']['sha256'])
  (root/'build/image-manifest.json').write_text(json.dumps(image))
  if change:change(root,receipt)
  run=subprocess.run([sys.executable,str(root/'scripts/package-release.py'),'--build-dir',str(root/'build'),'--output-dir',str(root/'release'),'--source-manifest',str(root/'source.json')],capture_output=True,text=True)
  return root,run
 def test_valid(self):
  root,run=self.fixture();self.assertEqual(run.returncode,0,run.stderr);self.assertEqual(len(list((root/'release').glob('*.zip'))),3)
 def test_stale_windows_binaries(self):
  for name in ('AutoblowLocal.exe','autoblow-server.exe','autoblow-catalog-import.exe'):
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
