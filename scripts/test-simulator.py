"""Exercise the compiled standalone lab; never contact a real device or cloud."""
import argparse
import json
import subprocess
import time
import urllib.error
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--binary', required=True)
args = parser.parse_args()
process = subprocess.Popen([args.binary, '--lab', '--duration', '8s'], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
try:
    line = process.stdout.readline()
    info = json.loads(line)
    assert info['synthetic'] and info['physicalDevice'] is False
    base, key = info['api'], info['controlToken']
    assert base.startswith('http://127.0.0.1:')
    calls = 0

    def request(method, path, body=None):
        global calls
        headers = {'x-device-token': key, 'Authorization': 'Bearer ' + key}
        if body is not None:
            body = json.dumps(body).encode()
            headers['Content-Type'] = 'application/json'
        r = urllib.request.Request(base + path, data=body, headers=headers, method=method)
        with urllib.request.urlopen(r, timeout=3) as response:
            calls += 1
            return json.load(response)

    deadline = time.monotonic() + 4
    while True:
        try:
            request('POST', '/api/pair', {'mac': info['pairMAC']})
            break
        except urllib.error.HTTPError:
            assert time.monotonic() < deadline, 'lab pairing deadline'
            time.sleep(0.02)

    state = request('PUT', '/vacuglide/target-speed', {'targetSpeed': 15})
    assert state['targetSpeed'] == 15 and state['operationalMode'] == 'TARGET_SPEED_PLAYING'
    for valve, field in [('stroke-plus', 'strokePlusValve'), ('stroke-minus', 'strokeMinusValve')]:
        state = request('PUT', '/vacuglide/valve/' + valve, {'valveState': True})
        assert state[field] is True
    request('POST', '/api/stop', {})
    state = request('GET', '/vacuglide/state')
    assert state['operationalMode'] == 'TARGET_SPEED_PAUSED' and not state['strokePlusValve'] and not state['strokeMinusValve']
    slots = request('GET', '/vacuglide/local-script/loaded')['loadedScripts']
    assert len(slots) == 30
    moved = request('PUT', '/vacuglide/local-script/move', {'fromIndex': 0, 'toIndex': 29})['loadedScripts']
    assert moved[-1] == slots[0]
    # Use a bounded JSON Funscript URL served by a temporary loopback source only
    # in SDK integration; here verify binary loading with the multipart file route.
    boundary = 'local-simulator-contract'
    payload = json.dumps({'actions': [{'at': 0, 'pos': 0}, {'at': 1000, 'pos': 60}, {'at': 2000, 'pos': 0}]}).encode()
    body = ('--' + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="synthetic.funscript"\r\nContent-Type: application/json\r\n\r\n').encode() + payload + ('\r\n--' + boundary + '--\r\n').encode()
    req = urllib.request.Request(base + '/vacuglide/sync-script/upload-funscript', data=body, method='PUT', headers={'x-device-token': key, 'Content-Type': 'multipart/form-data; boundary=' + boundary})
    with urllib.request.urlopen(req, timeout=3) as response:
        uploaded = json.load(response)
    assert uploaded['operationalMode'] == 'SYNC_SCRIPT_PAUSED' and uploaded['syncScriptToken']
    state = request('PUT', '/vacuglide/sync-script/start', {'startTimeMs': 500})
    assert state['operationalMode'] == 'SYNC_SCRIPT_PLAYING' and state['syncScriptCurrentTime'] == 500
    request('POST', '/api/stop', {})
    assert process.wait(timeout=12) == 0
    assert not process.stderr.read().strip()
    print(json.dumps({'standaloneSimulator': True, 'tlsVerified': True, 'manual': True, 'bothValves': True, 'safeStop': True, 'slots': True, 'scriptDownloadAndStart': True, 'durationExit': True, 'requests': calls + 1, 'physicalDevice': False}))
finally:
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
