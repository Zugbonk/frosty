"""Generate the Windows icon + VERSIONINFO .syso using Python 3 stdlib.

Go 1.23 embeds icon_windows.syso automatically; normal Windows builds do NOT
require Python. Rerun this only when the icon or product metadata changes.
"""
from pathlib import Path
import struct

root = Path(__file__).resolve().parents[1]
ico = (root / 'assets/app_icon.ico').read_bytes()
count = struct.unpack_from('<H', ico, 4)[0]
icons = []
for i in range(count):
    dimensions = struct.unpack_from('<BBBBHHII', ico, 6 + i * 16)
    size, position = dimensions[-2:]
    icons.append((dimensions, ico[position:position + size]))

group = bytearray(struct.pack('<HHH', 0, 1, count))
for i, (dims, data) in enumerate(icons, 1):
    w, h, colors, reserved, planes, bpp, size, pos = dims
    group.extend(struct.pack('<BBBBHHIH', w, h, colors, reserved, planes or 1, bpp, size, i))


def align4(data):
    return data + b'\0' * (-len(data) % 4)


def utf16(s):
    return s.encode('utf-16le') + b'\0\0'


def version_block(key, value=b'', children=(), is_text=False):
    """A Windows VS_VERSIONINFO block with 32-bit boundary padding."""
    beginning = b'\0' * 6 + utf16(key)
    beginning = align4(beginning)
    result = beginning + value
    result = align4(result)
    for child in children:
        result += align4(child)
    value_length = len(value) // 2 if is_text else len(value)
    return struct.pack('<HHH', len(result), value_length, 1 if is_text else 0) + result[6:]


def str_entry(key, value):
    return version_block(key, utf16(value), is_text=True)

strings = {
    'FileDescription': 'Wotlk 3.3.5a companion app',
    'FileVersion': '1.01',
    'ProductName': 'Frosty Zugbonk',
    'ProductVersion': '1.01',
    'CompanyName': 'www.zugbonk.com',
    'LegalCopyright': 'Copyright © 2026 Zugbonk',
    'InternalName': 'Frosty Zugbonk',
    'OriginalFilename': 'Frosty Zugbonk.exe',
    'Comments': 'https://www.zugbonk.com',
}
fixed = struct.pack('<13I',
    0xFEEF04BD, 0x00010000,
    0x00010000, 0x00010000,
    0x00010000, 0x00010000,
    0x0000003F, 0,
    0x00040004, 1, 0, 0, 0,
)
string_table = version_block('040904B0', children=[str_entry(k, v) for k, v in strings.items()], is_text=True)
string_info = version_block('StringFileInfo', children=[string_table], is_text=True)
translation = version_block('Translation', struct.pack('<HH', 0x0409, 1200))
var_info = version_block('VarFileInfo', children=[translation], is_text=True)
version = version_block('VS_VERSION_INFO', fixed, [string_info, var_info])
assert len(version) % 4 == 0

payloads = ([(3, i, payload) for i, (_, payload) in enumerate(icons, 1)]
            + [(14, 1, bytes(group)), (16, 1, version)])
rs = bytearray()


def alloc(size, alignment=4):
    while len(rs) % alignment:
        rs.append(0)
    start = len(rs)
    rs.extend(b'\0' * size)
    return start


def putdir(offset, entries):
    struct.pack_into('<IIHHHH', rs, offset, 0, 0, 0, 0, 0, len(entries))
    for i, (identifier, target, is_dir) in enumerate(entries):
        struct.pack_into('<II', rs, offset + 16 + i * 8, identifier,
                         target | (0x80000000 if is_dir else 0))

keys = sorted({kind for kind, _, _ in payloads})
root_dir = alloc(16 + 8 * len(keys))
kind_dirs = {kind: alloc(16 + 8 * sum(1 for k, _, _ in payloads if k == kind)) for kind in keys}
lang_dirs = {(kind, i): alloc(24) for kind, i, _ in payloads}
entries = {(kind, i): alloc(16) for kind, i, _ in payloads}
blobs = {}
for kind, i, payload in payloads:
    start = alloc(len(payload))
    rs[start:start + len(payload)] = payload
    blobs[(kind, i)] = (start, len(payload))
putdir(root_dir, [(kind, kind_dirs[kind], True) for kind in keys])
for kind in keys:
    putdir(kind_dirs[kind], [(i, lang_dirs[(kind, i)], True)
                              for k, i, _ in payloads if k == kind])
relocs = []
for key, lang in lang_dirs.items():
    putdir(lang, [(1033, entries[key], False)])
    off, size = blobs[key]
    struct.pack_into('<IIII', rs, entries[key], off, size, 0, 0)
    relocs.append((entries[key], 0, 7))

# PE32 COFF object, .rsrc section, IMAGE_REL_I386_DIR32NB relocations.
raw = 60
reloc_start = raw + len(rs)
symbol_start = reloc_start + len(relocs) * 10
header = struct.pack('<HHIIIHH', 0x14C, 1, 0, symbol_start, 2, 0, 0)
section = struct.pack('<8sIIIIIIHHI', b'.rsrc\0\0\0', 0, 0, len(rs), raw,
                      reloc_start, 0, len(relocs), 0, 0x40000040)
relocations = b''.join(struct.pack('<IIH', *item) for item in relocs)
symbol = struct.pack('<8sIhHBB', b'.rsrc\0\0\0', 0, 1, 0, 3, 1)
aux = struct.pack('<IHHIHBB', len(rs), len(relocs), 0, 0, 0, 0, 0) + b'\0\0'
assert len(aux) == 18
object_bytes = header + section + rs + relocations + symbol + aux + struct.pack('<I', 4)
(root / 'icon_windows.syso').write_bytes(object_bytes)
print(f'Generated icon_windows.syso: {count} icon sizes, one VERSIONINFO resource, {len(object_bytes)} bytes')
