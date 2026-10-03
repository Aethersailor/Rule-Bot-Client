#!/usr/bin/env python3
"""The Go ABI and package architecture map shared by release builders."""

import argparse
import json
from pathlib import Path
import struct
import subprocess


def catalog():
    return json.loads(Path(__file__).with_suffix('.json').read_text())


def package_rows(manager):
    return [(row[manager], row['variant']) for row in catalog()['packages'] if manager in row]


def check_binary(path, variant, commit, version):
    settings = catalog()['variants'][variant]
    data = Path(path).read_bytes()
    if data[:4] != b'\x7fELF':
        raise ValueError(f'not an ELF binary: {path}')
    arch = settings['goarch']
    little = arch not in ('mips', 'mips64')
    wide = arch in ('amd64', 'arm64', 'mips64', 'mips64le', 'riscv64', 'loong64')
    machine = {'amd64': 62, '386': 3, 'arm': 40, 'arm64': 183, 'mips': 8,
               'mipsle': 8, 'mips64': 8, 'mips64le': 8, 'riscv64': 243, 'loong64': 258}[arch]
    if data[4] != (2 if wide else 1) or data[5] != (1 if little else 2):
        raise ValueError(f'ELF class or byte order does not match {variant}: {path}')
    if struct.unpack_from(('<' if little else '>') + 'H', data, 18)[0] != machine:
        raise ValueError(f'ELF machine does not match {variant}: {path}')
    output = subprocess.check_output(['go', 'version', '-m', str(path)], text=True)
    build = {}
    for line in output.splitlines():
        if line.startswith('\tbuild\t') and '=' in line:
            key, value = line.removeprefix('\tbuild\t').split('=', 1)
            build[key] = value
    required = {'GOOS': 'linux', 'GOARCH': arch, 'CGO_ENABLED': '0'}
    if arch == 'amd64':
        required['GOAMD64'] = 'v1'
    if arch == 'arm':
        required['GOARM'] = settings['goarm']
    if arch == '386':
        required['GO386'] = settings['go386']
    if arch in ('mips', 'mipsle'):
        required['GOMIPS'] = settings['gomips']
    if arch in ('mips64', 'mips64le'):
        required['GOMIPS64'] = settings['gomips64']
    for key, value in required.items():
        actual = build.get(key, '').split(',', 1)[0]
        if actual != value:
            raise ValueError(f'{key}={actual!r}, expected {value!r} for {variant}: {path}')
    if arch == 'arm' and settings['goarm'] == '5' and 'hardfloat' in build['GOARM']:
        raise ValueError(f'ARMv5 software-float build required: {path}')
    if commit and commit.encode() not in data:
        raise ValueError(f'source identity is missing: {path}')
    if version and version.encode() not in data:
        raise ValueError(f'version identity is missing: {path}')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('command', choices=('stats', 'variants', 'packages', 'pairs', 'sdk', 'check-binary'))
    parser.add_argument('args', nargs='*')
    args = parser.parse_args()
    data = catalog()
    if args.command == 'stats':
        print(json.dumps({'variants': len(data['variants']), 'ipk': len(package_rows('ipk')),
                          'apk': len(package_rows('apk')), 'packages': sum(len(package_rows(m)) for m in ('ipk', 'apk'))}))
    elif args.command == 'variants':
        for name, row in data['variants'].items():
            print('\t'.join([name] + [row[key] or '-' for key in ('goarch', 'goarm', 'go386', 'gomips', 'gomips64')]))
    elif args.command == 'packages':
        for arch, variant in package_rows(args.args[0]):
            print(arch + '\t' + variant)
    elif args.command == 'pairs':
        for manager in ('ipk', 'apk'):
            for arch, variant in package_rows(manager):
                print(manager + ':' + arch)
    elif args.command == 'sdk':
        sdk = data['sdks'][args.args[0]]
        print('https://downloads.openwrt.org/releases/' + sdk['release'] + '/targets/' + sdk['target'] + '/' + sdk['subtarget'])
    else:
        path, variant, *identity = args.args
        check_binary(path, variant, *identity)


if __name__ == '__main__':
    main()
