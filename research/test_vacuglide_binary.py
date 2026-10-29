import json
import hashlib
from pathlib import Path
import unittest
from vacuglide_binary import decode, encode, from_funscript, from_csv


class BinaryCodecTest(unittest.TestCase):
    def test_official_captured_golden_vectors(self):
        path = Path(__file__).parent.parent / 'internal/protocol/testdata/sync-script-observed-20261027.json'
        vectors = json.loads(path.read_text())
        self.assertEqual(len(vectors), 11)
        for vector in vectors:
            binary = bytes.fromhex(vector['hex'])
            self.assertEqual(encode(decode(binary)), binary)
            self.assertEqual(encode(from_funscript(vector['actions'])), binary)

    def test_no_source_direction_or_tail_is_invented(self):
        forward = [{'at': 0, 'pos': 0}, {'at': 1000, 'pos': 60}]
        backward = [{'at': 0, 'pos': 60}, {'at': 1000, 'pos': 0}]
        shorter = [{'at': 0, 'pos': 0}, {'at': 500, 'pos': 30}]
        a, b = from_funscript(forward), from_funscript(backward)
        self.assertEqual(encode(a), encode(b))
        self.assertEqual(encode(a), encode(from_funscript(shorter)))
        self.assertIsNone(decode(encode(a))['tail_duration_ms'])

    def test_reserved_byte_is_preserved_for_analysis(self):
        binary = bytes.fromhex('0000000001000000000000000711')
        self.assertEqual(encode(decode(binary)), binary)
        self.assertEqual(decode(binary)['steps'][0]['operation'], 'unknown')
        self.assertIsNone(decode(binary)['steps'][0]['speed_percent'])

    def test_firmware_valve_opcodes_and_equal_timestamps(self):
        # Literal record layout from400d9e08, not a physical playback capture.
        binary = bytes.fromhex('7b00000001000000' '000000000101' '000000000201' '64000000000f' 'e80300000100' 'e80300000200')
        result = decode(binary)
        self.assertEqual(result['header'], [123, 1])
        self.assertEqual([x['operation'] for x in result['steps']], ['stroke-minus-valve', 'stroke-plus-valve', 'target-speed', 'stroke-minus-valve', 'stroke-plus-valve'])
        self.assertEqual([result['steps'][i]['valve_state'] for i in (0,1,3,4)], [True,True,False,False])
        self.assertEqual(encode(result), binary)
        # Native GPIO setter treats any nonzero value as open; preserve raw255.
        opened = decode(bytes.fromhex('0000000001000000' '0000000001ff'))
        self.assertTrue(opened['steps'][0]['valve_state'])
        self.assertEqual(encode(opened), bytes.fromhex('00000000010000000000000001ff'))

    def test_explicit_valve_authoring_and_conflicting_alias(self):
        script = {'header':[0,1], 'steps':[{'at_ms':0,'opcode':1,'value':1},{'at_ms':0,'opcode':2,'value':0}]}
        self.assertEqual(encode(script).hex(),'0000000001000000000000000101000000000200')
        script['steps'][0]['reserved'] = 0
        with self.assertRaises(ValueError):
            encode(script)

    def test_malformed_files_rejected(self):
        for binary in (b'', bytes(8), bytes.fromhex('0100000002000000000000000011'),
                       bytes.fromhex('0000000001000000000000000065')):
            with self.subTest(binary=binary), self.assertRaises(ValueError):
                decode(binary)

    def test_official_edge_corpus(self):
        path = Path(__file__).parent.parent / 'internal/protocol/testdata/cloud-converter-edge-20261028.json'
        cases = json.loads(path.read_text())
        self.assertEqual(len(cases), 80)
        for case in cases:
            with self.subTest(case=case['case']):
                def convert():
                    if case['format'] == 'csv':
                        return encode(from_csv(case['raw']))
                    data = case.get('input')
                    if case.get('generator'):
                        data = {'actions':[{'at':i,'pos':100*(i%2)} for i in range(case['generator']['count'])]}
                    if not isinstance(data, dict) or not isinstance(data.get('actions'),list):
                        raise ValueError('actions required')
                    return encode(from_funscript(data['actions']))
                if case['cloud_status'] == 400:
                    with self.assertRaises((ValueError,KeyError,OverflowError)):
                        convert()
                else:
                    binary = convert()
                    self.assertEqual(len(binary),case['bytes'])
                    self.assertEqual(hashlib.sha256(binary).hexdigest(),case['sha256'])

    def test_header_only_and_unordered_are_analysis_not_playback(self):
        self.assertEqual(encode(decode(bytes.fromhex('0000000001000000'))), bytes.fromhex('0000000001000000'))
        binary = bytes.fromhex('0000000001000000010000000011000000000011')
        self.assertFalse(decode(binary)['ordered_timeline'])
        self.assertEqual(encode(decode(binary)),binary)


if __name__ == '__main__':
    unittest.main()
