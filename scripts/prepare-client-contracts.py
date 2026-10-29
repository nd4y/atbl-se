"""Stage public SDK dependencies and a read-only snapshot of the Studio adapter."""
import argparse
import hashlib
import json
import pathlib
import shutil

parser = argparse.ArgumentParser()
parser.add_argument("--studio-root", type=pathlib.Path, help="Optional live Studio checkout; default uses the bundled interoperability snapshot")
parser.add_argument("--output", type=pathlib.Path, default=pathlib.Path("out/clients-integration"))
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
dest = args.output.resolve()
dest.mkdir(parents=True, exist_ok=True)
inputs = {
    "studio-httpClient.ts": "apps/server/src/autoblow/httpClient.ts",
    "studio-device-types.ts": "packages/shared/src/device/types.ts",
    "studio-types.ts": "packages/shared/src/types.ts",
}
provenance = {}
for name, relative in inputs.items():
    source = args.studio_root / relative if args.studio_root else root / "tests" / "clients" / name
    raw = source.read_bytes()
    provenance[name] = {"source": relative, "sha256": hashlib.sha256(raw).hexdigest()}
    if name == "studio-httpClient.ts" and args.studio_root:
        text = raw.decode("utf-8")
        original = "from '@vacuglide/shared'"
        if text.count(original) != 1:
            raise ValueError("Studio adapter import changed; review before staging")
        # Only module resolution changes, not the adapter's request behavior.
        raw = text.replace(original, "from './studio-device-types.ts'").encode("utf-8")
    (dest / name).write_bytes(raw)
for name in ("package.json", "package-lock.json"):
    shutil.copyfile(root / "tests/clients" / name, dest / name)
(dest / "studio-provenance.json").write_text(json.dumps(provenance, indent=2), encoding="utf-8")
print("Client contract sources staged; install the pinned dev dependencies with npm ci")
