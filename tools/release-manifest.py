#!/usr/bin/env python3
"""Create/verify a release inventory before installation; does not deploy files."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

NAME = 'release-manifest.json'

def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()

def inventory(root):
    result = {}
    for path in sorted(root.rglob('*')):
        if path.is_symlink():
            raise ValueError('release must not contain symlinks: ' + str(path))
        if path.is_file() and path.relative_to(root).as_posix() != NAME:
            result[path.relative_to(root).as_posix()] = digest(path)
    return result

def git(source, *args):
    p = subprocess.run(['git', '-C', str(source), *args], capture_output=True, text=True)
    return p.stdout.strip() if p.returncode == 0 else None

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['create', 'verify'])
    parser.add_argument('--root', required=True, type=Path)
    parser.add_argument('--source', type=Path)
    parser.add_argument('--version')
    args = parser.parse_args()
    root = args.root.resolve(strict=True)
    actual = inventory(root)
    if args.mode == 'create':
        if not args.source or not args.version:
            parser.error('create requires --source and --version')
        revision = git(args.source, 'rev-parse', 'HEAD')
        status = git(args.source, 'status', '--porcelain')
        manifest = {'schema_version': 1, 'release_version': args.version,
                    'source_revision': revision, 'source_dirty': bool(status) if status is not None else None,
                    'claude_reference_version': '2.1.276',
                    'platform_acceptance': 'not_attested_by_packaging',
                    'files': actual}
        (root / NAME).write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n')
        print(f'Created manifest: {len(actual)} files; no platform acceptance implied')
    else:
        manifest = json.loads((root / NAME).read_text())
        expected = manifest['files']
        if manifest.get('schema_version') != 1 or not isinstance(expected, dict):
            raise ValueError('unsupported manifest')
        changed = sorted(p for p in actual.keys() | expected.keys() if actual.get(p) != expected.get(p))
        if changed:
            raise ValueError('release inventory differs: ' + ', '.join(changed[:30]))
        print(f'PASS release inventory: {len(actual)} files')

if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError) as error:
        raise SystemExit(str(error))
