"""Bounded static ESP32 app image check. Not a flashing or signature tool."""
import hashlib,json,pathlib,struct,sys,functools
def inspect(path):
 data=path.read_bytes();assert 24<len(data)<=8*1024*1024
 assert data[0]==0xe9 and 1<=data[1]<=16
 count=data[1];position=24;checksum=0xef;segments=[];app=None
 for number in range(count):
  assert position+8<=len(data)
  address,size=struct.unpack_from('<II',data,position);position+=8
  assert position+size<=len(data) and size<=8*1024*1024
  block=data[position:position+size]
  checksum=functools.reduce(lambda a,b:a^b,block,checksum)
  segments.append({'index':number,'load_address':hex(address),'bytes':size,'file_offset':position})
  if number==0 and len(block)>=256 and struct.unpack_from('<I',block)[0]==0xabcd5432:
   def text(offset,length):return block[offset:offset+length].split(b'\0',1)[0].decode('ascii','replace')
   app={'magic':'0xabcd5432','secure_version':struct.unpack_from('<I',block,4)[0],
        'version':text(16,32),'project_name':text(48,32),'build_time':text(80,16),
        'build_date':text(96,16),'idf_version':text(112,32)}
  position+=size
 checksum_offset=(position//16+1)*16-1
 assert checksum_offset<len(data) and data[checksum_offset]==checksum
 appended_hash=data[23]==1
 image_end=checksum_offset+1
 digest_valid=None
 if appended_hash:
  assert image_end+32<=len(data)
  digest_valid=hashlib.sha256(data[:image_end]).digest()==data[image_end:image_end+32]
  assert digest_valid;image_end+=32
 assert app is not None
 # Print only format/descriptor fields, never arbitrary image strings or credentials.
 return {'file':path.name,'bytes':len(data),'sha256':hashlib.sha256(data).hexdigest(),
         'segment_count':count,'segments':segments,'entry_address':hex(struct.unpack_from('<I',data,4)[0]),
         'chip_id':struct.unpack_from('<H',data,12)[0],'checksum_valid':True,
         'appended_hash':appended_hash,'appended_sha256_valid':digest_valid,
         'trailing_bytes':len(data)-image_end,'app':app,'signature_authenticity':'not assessed'}
if __name__=='__main__':
 results=[inspect(pathlib.Path(p)) for p in sys.argv[1:]]
 print(json.dumps(results,indent=2))
