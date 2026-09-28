import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location('publisher', Path(__file__).with_name('publish-docs.py'))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)

class PublisherTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / 'guide.json'
        self.source.write_text(json.dumps({'schemaVersion': 1, 'sections': [{'id': 'install', 'title': 'Install', 'paragraphs': ['Install {{version}}']}]}))
        self.dest = self.root / 'public'

    def publish(self, version='1.0.0'):
        publisher.publish(self.source, self.dest, version, 'a' * 40)

    def test_snapshot_version_template_index_and_idempotence(self):
        self.publish()
        before = (self.dest / 'docs/1.0.0.json').read_bytes()
        self.publish()
        self.assertEqual(before, (self.dest / 'docs/1.0.0.json').read_bytes())
        snapshot = json.loads(before)
        self.assertEqual(snapshot['sections'][0]['paragraphs'], ['Install 1.0.0'])
        self.assertEqual(snapshot['sourceTag'], 'v1.0.0')
        self.assertEqual(snapshot['sourceCommit'], 'a' * 40)
        self.publish('1.1.0-beta.2')
        self.assertEqual(json.loads((self.dest / 'docs/index.json').read_text())['versions'], ['1.0.0', '1.1.0-beta.2'])
        self.assertEqual(before, (self.dest / 'docs/1.0.0.json').read_bytes())

    def test_same_guide_keeps_the_original_commit(self):
        self.publish()
        before = (self.dest / 'docs/1.0.0.json').read_bytes()
        publisher.publish(self.source, self.dest, '1.0.0', 'b' * 40)
        self.assertEqual(before, (self.dest / 'docs/1.0.0.json').read_bytes())

    def test_retag_cannot_rewrite_snapshot(self):
        self.publish()
        self.source.write_text(self.source.read_text().replace('Install {{version}}', 'Changed'))
        with self.assertRaisesRegex(ValueError, 'immutable'):
            self.publish()

    def test_invalid_version_and_sections_do_not_publish(self):
        for version in ('../escape', 'v1.0.0', '1.0', '01.0.0'):
            with self.assertRaises(ValueError):
                self.publish(version)
        self.source.write_text('{"schemaVersion":1,"sections":[]}')
        with self.assertRaises(ValueError):
            self.publish()
        self.assertFalse((self.dest / 'docs/index.json').exists())

if __name__ == '__main__':
    unittest.main()
