#!/usr/bin/env python3
"""Freeze the tagged guide into a checked-out public releases repository."""
import argparse
import json
import re
from pathlib import Path

VERSION = re.compile(r"(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?")

def publish(source, destination, version, commit):
    if not VERSION.fullmatch(version):
        raise ValueError('Expected a release version without v or build metadata')
    if not re.fullmatch(r'[0-9a-f]{40}', commit):
        raise ValueError('Expected the full source commit SHA')
    guide = json.loads(Path(source).read_text().replace('{{version}}', version))
    if guide.get('schemaVersion') != 1 or not isinstance(guide.get('sections'), list) or not guide['sections']:
        raise ValueError('Guide needs schemaVersion 1 and nonempty sections')
    ids = set()
    for section in guide['sections']:
        sid = section.get('id', '')
        if not re.fullmatch(r'[a-z][a-z0-9-]*', sid) or sid in ids:
            raise ValueError('Section IDs must be unique anchors')
        ids.add(sid)
        if not isinstance(section.get('title'), str) or not section['title'].strip():
            raise ValueError('Section title is required')
        for key in ('paragraphs', 'steps'):
            values = section.get(key, [] if key == 'steps' else None)
            if not isinstance(values, list) or (key == 'paragraphs' and not values) or any(not isinstance(v, str) or not v.strip() for v in values):
                raise ValueError(f'Invalid {key}')
        if 'code' in section and not isinstance(section['code'], str):
            raise ValueError('Code must be text')
    snapshot = dict(guide, version=version, sourceTag='v' + version, sourceCommit=commit)
    encoded = json.dumps(snapshot, indent=2) + '\n'
    if '{{' in encoded:
        raise ValueError('Unresolved template placeholder')
    root = Path(destination) / 'docs'
    index_path = root / 'index.json'
    index = json.loads(index_path.read_text()) if index_path.exists() else {'schemaVersion': 1, 'versions': []}
    if index.get('schemaVersion') != 1 or not isinstance(index.get('versions'), list):
        raise ValueError('Invalid existing documentation index')
    if any(not isinstance(v, str) or not VERSION.fullmatch(v) for v in index['versions']):
        raise ValueError('Invalid existing documentation version')
    root.mkdir(parents=True, exist_ok=True)
    path = root / (version + '.json')
    if path.exists():
        current = path.read_text()
        if current != encoded:
            published = json.loads(current)
            proposed = json.loads(encoded)
            published.pop('sourceCommit', None)
            proposed.pop('sourceCommit', None)
            if published != proposed:
                raise ValueError(f'Refusing to overwrite immutable documentation for {version}')
            encoded = current
    path.write_text(encoded)
    index['versions'] = sorted(set(index['versions'] + [version]))
    index_path.write_text(json.dumps(index, indent=2) + '\n')

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', default='docs/user-guide/guide.json')
    parser.add_argument('--destination', required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--commit', required=True)
    args = parser.parse_args()
    publish(args.source, args.destination, args.version, args.commit)
