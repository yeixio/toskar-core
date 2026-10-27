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

    def test_images_are_published_and_paths_stay_in_the_user_guide(self):
        guide = json.loads(self.source.read_text())
        guide['sections'][0]['images'] = [{
            'src': 'docs/user-guide/images/07-automations.png',
            'alt': 'Automations list',
            'caption': 'The scheduled task and its last result.',
        }]
        self.source.write_text(json.dumps(guide))
        self.publish()
        snapshot = json.loads((self.dest / 'docs/1.0.0.json').read_text())
        self.assertEqual(snapshot['sections'][0]['images'][0]['src'], 'docs/user-guide/images/07-automations.png')
        guide['sections'][0]['images'][0]['src'] = 'docs/screenshots/07-automations.png'
        self.source.write_text(json.dumps(guide))
        with self.assertRaisesRegex(ValueError, 'docs/user-guide/images'):
            publisher.publish(self.source, self.dest, '1.2.0', 'c' * 40)

    def detailed(self, **extra):
        section = {
            'id': 'install', 'title': 'Install', 'paragraphs': ['Install {{version}}'],
            'summary': 'Get Toskar running.', 'kind': 'task',
            'blocks': [
                {'type': 'list', 'title': 'What you need', 'items': ['A computer with 16 GB of memory']},
                {'type': 'heading', 'text': 'Steps'},
                {'type': 'steps', 'items': ['Open the app.', 'Choose a model, as [Models](#models) explains.']},
                {'type': 'note', 'tone': 'tip', 'text': 'See [the releases](https://github.com/yeixio/toskar-core/releases).'},
                {'type': 'image', 'src': 'docs/user-guide/images/07-automations.png', 'alt': 'Automations', 'caption': 'The list.'},
                {'type': 'code', 'language': 'sh', 'code': 'toskarctl ask "hi"', 'test': 'ask-hi', 'output': 'Hello'},
                {'type': 'table', 'columns': ['Memory', 'Model'], 'rows': [['16 GB', 'Small']]},
                {'type': 'related', 'sections': ['models']},
            ],
        }
        section.update(extra)
        models = {'id': 'models', 'title': 'Models', 'paragraphs': ['Pick one.']}
        self.source.write_text(json.dumps({'schemaVersion': 1, 'sections': [section, models]}))

    def test_detailed_sections_publish_their_blocks(self):
        self.detailed()
        self.publish()
        section = json.loads((self.dest / 'docs/1.0.0.json').read_text())['sections'][0]
        self.assertEqual(section['kind'], 'task')
        self.assertEqual([b['type'] for b in section['blocks']], ['list', 'heading', 'steps', 'note', 'image', 'code', 'table', 'related'])

    def test_malformed_blocks_do_not_publish(self):
        bad = [
            {'type': 'script', 'text': 'x'},
            {'type': 'heading', 'text': ' '},
            {'type': 'note', 'tone': 'danger', 'text': 'x'},
            {'type': 'code', 'language': 'html', 'code': '<b>'},
            {'type': 'code', 'language': 'sh', 'code': 'ls', 'test': 'Bad Id'},
            {'type': 'table', 'columns': ['A', 'B'], 'rows': [['only one']]},
            {'type': 'image', 'src': 'https://example.com/x.png', 'alt': 'x', 'caption': 'x'},
            {'type': 'related', 'sections': ['nowhere']},
        ]
        for block in bad:
            with self.subTest(block=block):
                self.detailed(blocks=[block])
                with self.assertRaises(ValueError):
                    self.publish()
        self.detailed(kind='tutorial')
        with self.assertRaisesRegex(ValueError, 'Kind'):
            self.publish()

    def test_links_go_to_sections_or_https_pages(self):
        for paragraph, error in (('See [Gone](#gone).', 'missing section'), ('See [x](javascript:alert(1)).', 'https'), ('See [x](http://example.com).', 'https')):
            with self.subTest(paragraph=paragraph):
                self.detailed(paragraphs=[paragraph])
                with self.assertRaisesRegex(ValueError, error):
                    self.publish()

if __name__ == '__main__':
    unittest.main()
