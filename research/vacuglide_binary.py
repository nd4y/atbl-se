"""Offline codec for VacuGlide 1.03 speed and valve event records.

No device or network access. Decoding preserves speed setpoints, not the source
Funscript positions. Opcode semantics come from firmware handler400d9e08;
unknown opcodes are retained as raw bytes and never interpreted as speed.
"""
import argparse
import csv
import json
import math
import re
from pathlib import Path
import struct

HEADER = (0, 1)
MAX_TIME_MS = 16777214
MAX_BINARY_TIME_MS = 0xffffffff
OPERATIONS = {0: 'target-speed', 1: 'stroke-minus-valve', 2: 'stroke-plus-valve'}


def decode(data):
    if len(data) < 8 or (len(data) - 8) % 6:
        raise ValueError("expected eight-byte header and six-byte records")
    header = struct.unpack_from('<II', data)
    if header[1] != HEADER[1]:
        raise ValueError("unrecognized header; do not assume this codec applies")
    steps = []
    previous = -1
    for offset in range(8, len(data), 6):
        at, opcode, value = struct.unpack_from('<IBB', data, offset)
        if opcode == 0 and value > 100:
            raise ValueError("invalid time order or speed")
        step = {'at_ms': at, 'opcode': opcode, 'operation': OPERATIONS.get(opcode, 'unknown'),
                'value': value, 'speed_percent': value if opcode == 0 else None,
                'reserved': opcode}  # Legacy JSON alias, no longer treated as reserved.
        if opcode in (1, 2):
            step['valve_state'] = bool(value)
        steps.append(step)
        previous = at
    return {'format': 'vacuglide-event-timeline', 'header': list(header),
            'tail_duration_ms': None, 'ordered_timeline': all(a['at_ms'] <= b['at_ms'] for a,b in zip(steps,steps[1:])), 'steps': steps}


def encode(script):
    header = script.get('header')
    if not isinstance(header, list) or len(header) != 2 or any(type(x) is not int for x in header) or not 0 <= header[0] <= 0xffffffff or header[1] != 1:
        raise ValueError("unrecognized header")
    steps = script.get('steps', [])
    if not 0 <= len(steps) <= 99999:
        raise ValueError("unsupported number of records")
    data = bytearray(struct.pack('<II', *header))
    previous = -1
    for step in steps:
        at = step['at_ms']
        opcode = step.get('opcode', step.get('reserved', 0))
        value = step.get('value', step.get('speed_percent'))
        if 'opcode' in step and 'reserved' in step and step['opcode'] != step['reserved']:
            raise ValueError("conflicting opcode and legacy reserved alias")
        if any(type(x) is not int for x in (at, opcode, value)):
            raise ValueError("record fields must be integers")
        if not 0 <= at <= MAX_BINARY_TIME_MS or not 0 <= value <= 255 or not 0 <= opcode <= 255 or opcode == 0 and value > 100:
            raise ValueError("invalid record field")
        data.extend(struct.pack('<IBB', at, opcode, value))
        previous = at
    return bytes(data)


JS_SPACE = "\u0009\u000a\u000b\u000c\u000d \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"


def _array_string(v):
    if v is None:
        return ''
    if isinstance(v, list):
        return ','.join(_array_string(x) for x in v)
    if isinstance(v, dict):
        if 'toString' in v:
            raise ValueError('Cannot convert object to primitive value')
        return '[object Object]'
    if isinstance(v, bool):
        return 'true' if v else 'false'
    return str(v)


def _number(v):
    if v is None:
        return 0.0
    if isinstance(v, (int, float)):
        return float(v)
    if not isinstance(v, str):
        v = _array_string(v)
    v = v.strip(JS_SPACE)
    if not v:
        return 0.0
    if v in ('Infinity', '+Infinity', '-Infinity'):
        return float(v.replace('Infinity', 'inf'))
    if re.fullmatch(r'0[xX][0-9a-fA-F]+|0[oO][0-7]+|0[bB][01]+', v):
        return float(int(v, 0))
    if not re.fullmatch(r'[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?', v):
        return 0.0
    return float(v)


def from_funscript(actions):
    """Official cloud packing/rounding; conversion does not admit device playback."""
    if not isinstance(actions, list) or not 1 <= len(actions) <= 100000:
        raise ValueError('expected one to100000 source actions')
    converted = []
    for a in actions:
        if 'at' not in a or 'pos' not in a:
            raise ValueError('each action requires at and pos')
        converted.append((_number(a['at']), _number(a['pos'])))
    if converted[-1][0] > MAX_TIME_MS:
        raise ValueError('source endpoint exceeds cloud time limit')
    points = []
    for at, pos in converted:
        at = 0 if math.isnan(at) else at
        pos = 0 if math.isnan(pos) else pos
        if not 0 <= at <= 0xffffffff or not 0 <= pos <= 255:
            raise ValueError('source point outside packed representation')
        p = (int(at) & 0xffffff, (int(pos)+128) % 256 - 128)
        if points and points[-1][0] == p[0]:
            points[-1] = p
        else:
            points.append(p)
    steps = []
    for a, b in zip(points, points[1:]):
        seconds = abs(b[0]-a[0])/1000
        velocity = abs(b[1]-a[1])/seconds
        value = (velocity/600)*100
        lower = math.floor(value)
        speed = min(100, lower + (value-lower >= 0.5))
        steps.append({'at_ms':a[0], 'opcode':0, 'value':speed})
    return decode(encode({'header':list(HEADER), 'steps':steps}))


def from_csv(raw):
    """Stock upload CSV uses parseInt prefixes and drops the unterminated tail."""
    lines = raw.split('\n')
    if len(lines) < 2:
        raise ValueError('No lines found in CSV script')
    actions = []
    def integer(s):
        s = s.lstrip(JS_SPACE)
        sign = -1 if s.startswith('-') else 1
        if s.startswith(('-', '+')):
            s = s[1:]
        base = 16 if s.startswith(('0x','0X')) else 10
        if base == 16:
            s = s[2:]
        match = re.match(r'[0-9a-fA-F]+' if base == 16 else r'[0-9]+', s)
        return sign*int(match.group(), base) if match else 0
    for line in lines[:-1]:
        fields = line.split(',')
        if len(fields) != 2:
            raise ValueError('Invalid CSV line: '+line)
        actions.append({'at':integer(fields[0]),'pos':integer(fields[1])})
    return from_funscript(actions)


def write_csv(script, path):
    # This is an analysis CSV, not the official upload-csv position format.
    with Path(path).open('w', encoding='utf-8', newline='') as stream:
        writer = csv.DictWriter(stream, fieldnames=['at_ms', 'opcode', 'operation', 'value', 'speed_percent', 'valve_state', 'reserved'], extrasaction='ignore')
        writer.writeheader()
        writer.writerows(script['steps'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=['decode', 'encode', 'funscript'])
    parser.add_argument('source', type=Path)
    parser.add_argument('output', type=Path)
    parser.add_argument('--csv', type=Path)
    args = parser.parse_args()
    if args.output.exists() or args.csv and args.csv.exists():
        parser.error('output already exists')
    if args.operation == 'decode':
        script = decode(args.source.read_bytes())
        args.output.write_text(json.dumps(script, indent=2)+'\n')
        if args.csv:
            write_csv(script, args.csv)
    else:
        script = json.loads(args.source.read_text())
        if args.operation == 'funscript':
            script = from_funscript(script['actions'])
        args.output.write_bytes(encode(script))


if __name__ == '__main__':
    main()
