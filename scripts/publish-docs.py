#!/usr/bin/env python3
"""Freeze the tagged guide into a checked-out public releases repository."""
import argparse
import json
import re
from pathlib import Path

VERSION = re.compile(r"(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?")
SECTION_ID = re.compile(r'[a-z][a-z0-9-]*')
# Inline text may hold `code` and [links](#section) or [links](https://...);
# the site renders those and nothing else, never HTML.
LINK = re.compile(r'\[([^\]\n]+)\]\(([^)\s]*)\)')
KINDS = ('task', 'concept', 'reference')
TONES = ('tip', 'warning')
LANGUAGES = ('sh', 'json', 'python', 'typescript', 'go', 'text')

def text(value):
    return isinstance(value, str) and value.strip() != ''

def texts(value):
    return isinstance(value, list) and len(value) > 0 and all(text(v) for v in value)

def screen_src(screen):
    """Where scripts/capture-docs-screenshots.sh saves a screen's screenshot."""
    return f'docs/user-guide/images/{screen}.png'

def check_image(image):
    if not isinstance(image, dict):
        raise ValueError('Image must be an object')
    # A generated screenshot names its screen (docs/user-guide/screens.json);
    # the snapshot gets its path, so the site needs nothing more.
    if 'screen' in image:
        if not isinstance(image['screen'], str) or not SECTION_ID.fullmatch(image['screen']):
            raise ValueError('Image screen must be a screen id')
        if image.setdefault('src', screen_src(image['screen'])) != screen_src(image['screen']):
            raise ValueError('An image names a screen or a src, not both')
    for key in ('src', 'alt', 'caption'):
        if not text(image.get(key, '')):
            raise ValueError(f'Image {key} is required')
    src = image['src']
    if src.startswith(('/', '\\')) or '://' in src or '..' in src.split('/'):
        raise ValueError('Image path must stay inside the repository docs')
    if not src.startswith('docs/user-guide/images/') or not src.endswith('.png'):
        raise ValueError('Image path must be a PNG under docs/user-guide/images')

def check_block(block):
    """One block of a detailed section; see docs/user-guide/README.md."""
    if not isinstance(block, dict):
        raise ValueError('Block must be an object')
    kind = block.get('type')
    if kind in ('heading', 'paragraph'):
        ok = text(block.get('text'))
    elif kind == 'steps':
        ok = texts(block.get('items'))
    elif kind == 'list':
        ok = texts(block.get('items')) and ('title' not in block or text(block['title']))
    elif kind == 'note':
        ok = block.get('tone') in TONES and text(block.get('text'))
    elif kind == 'image':
        check_image(block)
        ok = True
    elif kind == 'code':
        ok = (block.get('language') in LANGUAGES and text(block.get('code'))
              and ('test' not in block or (isinstance(block['test'], str) and SECTION_ID.fullmatch(block['test'])))
              and ('output' not in block or text(block['output'])))
    elif kind == 'table':
        columns, rows = block.get('columns'), block.get('rows')
        ok = texts(columns) and isinstance(rows, list) and len(rows) > 0 and all(
            isinstance(row, list) and len(row) == len(columns) and all(isinstance(cell, str) for cell in row) for row in rows)
    elif kind == 'related':
        ok = texts(block.get('sections'))
    else:
        raise ValueError(f'Unknown block type {kind!r}')
    if not ok:
        raise ValueError(f'Invalid {kind} block')

def inline_texts(section):
    """Every string a reader sees in a section, for link and wording checks."""
    out = [section['title'], *section['paragraphs'], *section.get('steps', [])]
    if text(section.get('summary', '')):
        out.append(section['summary'])
    for image in section.get('images', []):
        out += [image['alt'], image['caption']]
    for block in section.get('blocks', []):
        for key in ('text', 'title', 'alt', 'caption'):
            if text(block.get(key, '')):
                out.append(block[key])
        out += block.get('items', []) + block.get('columns', [])
        for row in block.get('rows', []):
            out += row
    return out

def validate(guide):
    if guide.get('schemaVersion') != 1 or not isinstance(guide.get('sections'), list) or not guide['sections']:
        raise ValueError('Guide needs schemaVersion 1 and nonempty sections')
    ids = set()
    for section in guide['sections']:
        sid = section.get('id', '')
        if not SECTION_ID.fullmatch(sid) or sid in ids:
            raise ValueError('Section IDs must be unique anchors')
        ids.add(sid)
        if not text(section.get('title')):
            raise ValueError('Section title is required')
        for key in ('paragraphs', 'steps'):
            values = section.get(key, [] if key == 'steps' else None)
            if not isinstance(values, list) or (key == 'paragraphs' and not values) or any(not text(v) for v in values):
                raise ValueError(f'Invalid {key}')
        if 'code' in section and not isinstance(section['code'], str):
            raise ValueError('Code must be text')
        images = section.get('images', [])
        if not isinstance(images, list):
            raise ValueError('Images must be a list')
        for image in images:
            check_image(image)
        if 'summary' in section and not text(section['summary']):
            raise ValueError('Summary must be text')
        if 'kind' in section and section['kind'] not in KINDS:
            raise ValueError(f'Kind must be one of {", ".join(KINDS)}')
        blocks = section.get('blocks', [])
        if not isinstance(blocks, list):
            raise ValueError('Blocks must be a list')
        for block in blocks:
            check_block(block)
    for section in guide['sections']:
        for block in section.get('blocks', []):
            for target in block.get('sections', []):
                if target not in ids:
                    raise ValueError(f'Related section {target!r} does not exist')
        for value in inline_texts(section):
            for _, target in LINK.findall(value):
                if target.startswith('#'):
                    if target[1:] not in ids:
                        raise ValueError(f'Link to missing section {target}')
                elif not target.startswith('https://'):
                    raise ValueError(f'Links go to a section (#id) or an https page, not {target!r}')

def publish(source, destination, version, commit):
    if not VERSION.fullmatch(version):
        raise ValueError('Expected a release version without v or build metadata')
    if not re.fullmatch(r'[0-9a-f]{40}', commit):
        raise ValueError('Expected the full source commit SHA')
    guide = json.loads(Path(source).read_text().replace('{{version}}', version))
    validate(guide)
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
