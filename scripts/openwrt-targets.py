#!/usr/bin/env python3
"""The Go ABI and package architecture map shared by release builders."""

import argparse
import json
from pathlib import Path


def catalog():
    return json.loads(Path(__file__).with_suffix('.json').read_text())


def package_rows(manager):
    return [(row[manager], row['variant']) for row in catalog()['packages'] if manager in row]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('command', choices=('stats', 'variants', 'packages', 'pairs', 'sdk'))
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


if __name__ == '__main__':
    main()
