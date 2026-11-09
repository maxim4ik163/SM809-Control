#!/usr/bin/env python3
"""Rebuild the zones firmware and put it into an updater exe.

usage: build_fw.py ORIGINAL.exe UIBASE.exe OUT.exe
  ORIGINAL.exe  vendor updater SM809_Mouse_Wired_USB_IAP25-08-11-1.exe (stock firmware is taken from it)
  UIBASE.exe    an updater with our UI patches (English, forced, 30 s) — e.g. the patcher's output
  OUT.exe       UIBASE with the rebuilt firmware

Then: python3 ../mkpatch.py ORIGINAL.exe OUT.exe > ../patch_data.go
Needs llvm-mc-18 and llvm-objcopy-18 (zones.s, Thumb, ARMv5TE).

Firmware container (resource at file offset 0x1d4eb8, 0x1adc0 bytes):
  [0:4] CS1 (checked by the updater, see tool_crc), [4:16] header,
  then 32-byte blocks of the app image (load address 0x28800), each + CRC-16/0x8005 big endian;
  all-FF blocks that had FF FF as CRC in the original stay that way (the updater skips them).
"""
import struct, subprocess, sys, os, tempfile

RES_OFF, B = 0x1d4eb8, 0x28800

def crc16(data):
    c = 0xffff
    for byte in data:
        c ^= byte << 8
        for _ in range(8):
            c = ((c << 1) ^ 0x8005) & 0xffff if c & 0x8000 else (c << 1) & 0xffff
    return c

TBL = []
for n in range(256):
    c = n
    for _ in range(8):
        c = (c >> 1) ^ 0xedb88320 if c & 1 else c >> 1
    TBL.append(c)

def tool_crc(buf):  # the updater's CS1 check (0x4043c0)
    L = len(buf); edi = 0xffffffff; pos = 16
    for _ in range((L >> 4) - 1):
        for b in buf[pos:pos + 16]:
            edi = (edi >> 8) ^ TBL[(b ^ edi) & 0xff]
        pos += 16
    for b in buf[pos:pos + (L & 0xf)]:
        edi = (edi >> 8) ^ TBL[(b ^ edi) & 0xff]
    return edi

def bl(src, dst):
    off = (dst - (src + 4)) >> 1 & 0x3fffff
    return struct.pack('<HH', 0xf000 | (off >> 11), 0xf800 | (off & 0x7ff))

orig_exe = open(sys.argv[1], 'rb').read()
ui = bytearray(open(sys.argv[2], 'rb').read())
# the container length is fixed: 16 + n*34 + 10
L = 0x1adc0
cont = orig_exe[RES_OFF:RES_OFF + L]
assert tool_crc(cont) == struct.unpack_from('<I', cont)[0], 'not the expected vendor updater'
raw = bytearray(); o = 16
while o + 34 <= L:
    raw += cont[o:o + 32]; o += 34
assert len(raw) == 0x19460

def rd16(a): return struct.unpack_from('<H', raw, a - B)[0]
# d64: one lighting command (retries 5 -> 1), first send after 0x40 ticks
assert rd16(0x2dc90) == 0x230a and rd16(0x2dc9a) == 0x2705
struct.pack_into('<H', raw, 0x2dc90 - B, 0x2340); struct.pack_into('<H', raw, 0x2dc9a - B, 0x2701)
# zones code at 0x41c00 (erased space at the end of the image)
d = tempfile.mkdtemp()
src = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'zones.s')
subprocess.check_call(['llvm-mc-18', '-triple=thumbv5te-none-eabi', '-filetype=obj', src, '-o', d + '/z.o'])
subprocess.check_call(['llvm-objcopy-18', '-O', 'binary', '--only-section=.text', d + '/z.o', d + '/z.bin'])
code = open(d + '/z.bin', 'rb').read()
NEW = 0x41c00
assert raw[NEW - B:0x41c60 - B] == b'\xff' * 0x60 and len(code) <= 0x60
raw[NEW - B:NEW - B + len(code)] = code
assert rd16(0x2dc74) == 0x48c1 and rd16(0x2dc76) == 0x7f00   # ldr r0,=0x4015f8 ; ldrb r0,[r0,#0x1c]
raw[0x2dc74 - B:0x2dc78 - B] = bl(0x2dc74, NEW)               # side
assert rd16(0x2d4d2) == 0x9803 and rd16(0x2d4d4) == 0x2801   # ldr r0,[sp,#0xc] ; cmp r0,#1
raw[0x2d4d2 - B:0x2d4d6 - B] = bl(0x2d4d2, NEW + 0x0e)        # wheel / logo
# rebuild the container
out = bytearray(cont[:16])
for i in range(0, len(raw), 32):
    blk = raw[i:i + 32]; o = 16 + (i // 32) * 34
    if blk == b'\xff' * 32 and cont[o + 32:o + 34] == b'\xff\xff':
        out += blk + b'\xff\xff'
    else:
        c = crc16(blk); out += blk + bytes([c >> 8, c & 0xff])
out += cont[len(out):]
assert len(out) == L
struct.pack_into('<I', out, 0, tool_crc(out))
ui[RES_OFF:RES_OFF + L] = out
# PE checksum
pe = struct.unpack_from('<I', ui, 0x3c)[0]; oh = pe + 24
struct.pack_into('<I', ui, oh + 64, 0)
cs = sum(struct.unpack('<%dH' % (len(ui) // 2), bytes(ui)))
while cs >> 16:
    cs = (cs & 0xffff) + (cs >> 16)
struct.pack_into('<I', ui, oh + 64, cs + len(ui))
open(sys.argv[3], 'wb').write(ui)
print('CS1 %08x' % struct.unpack_from('<I', out)[0])
