import importlib.util
import json
import struct
import tempfile
import unittest
import zlib
from pathlib import Path

spec = importlib.util.spec_from_file_location('checker', Path(__file__).with_name('check-user-guide.py'))
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)


def png(width, height=10):
    def chunk(kind, data):
        return struct.pack('>I', len(data)) + kind + data + struct.pack('>I', zlib.crc32(kind + data))
    raw = b''.join(b'\x00' + b'\x00' * width * 3 for _ in range(height))
    return (b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', width, height, 8, 2, 0, 0, 0))
            + chunk(b'IDAT', zlib.compress(raw)) + chunk(b'IEND', b''))


class CheckTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.images = self.root / 'docs/user-guide/images'
        self.images.mkdir(parents=True)
        (self.images / 'chat.png').write_bytes(png(1280))
        self.screens(['chat'])

    def screens(self, ids):
        (self.root / 'docs/user-guide/screens.json').write_text(json.dumps({'screens': [{'id': i} for i in ids]}))

    def section(self, **extra):
        section = {
            'id': 'chat', 'title': 'Chat', 'paragraphs': ['Ask a question and read the answer.'],
            'summary': 'Talk to the model on this computer.', 'kind': 'task',
            'blocks': [
                {'type': 'list', 'title': 'What you need', 'items': ['A model, from [Models](#models).']},
                {'type': 'steps', 'items': ['Open Chat.', 'Type a question.', 'Press Send.']},
                {'type': 'image', 'screen': 'chat', 'alt': 'Chat', 'caption': 'An answer with its sources.'},
                {'type': 'heading', 'text': 'Troubleshooting'},
                {'type': 'paragraph', 'text': 'No answer? Check that a model is installed.'},
            ],
        }
        section.update(extra)
        return {'schemaVersion': 1, 'sections': [section, {'id': 'models', 'title': 'Models', 'paragraphs': ['Pick one.']}]}

    def check(self, guide):
        return checker.check(guide, self.root)

    def test_a_detailed_task_passes_and_legacy_sections_wait(self):
        problems, waiting = self.check(self.section())
        self.assertEqual(problems, [])
        self.assertEqual(waiting, ['models'])

    def test_brand_names_fail_anywhere(self):
        guide = self.section()
        guide['sections'][1]['paragraphs'] = ['Open it on your iPhone.']
        problems, _ = self.check(guide)
        self.assertTrue(any("'iPhone'" in p for p in problems), problems)

    def test_a_detailed_section_needs_what_the_style_asks(self):
        guide = self.section(blocks=[{'type': 'paragraph', 'text': 'Just words.'}])
        del guide['sections'][0]['summary']
        problems, _ = self.check(guide)
        text = '\n'.join(problems)
        for expected in ('summary', 'screenshot or a tested example', 'What you need', 'Troubleshooting'):
            self.assertIn(expected, text)

    def test_an_untested_code_block_doesnt_count_but_a_tested_one_does(self):
        blocks = [b for b in self.section()['sections'][0]['blocks'] if b['type'] != 'image']
        self.screens([])
        (self.images / 'chat.png').unlink()
        untested = self.section(blocks=blocks + [{'type': 'code', 'language': 'sh', 'code': 'toskarctl ask hi'}])
        self.assertTrue(any('tested example' in p for p in self.check(untested)[0]))
        tested = self.section(blocks=blocks + [{'type': 'code', 'language': 'sh', 'code': 'toskarctl ask hi', 'test': 'ask'}])
        self.assertEqual(self.check(tested)[0], [])

    def legacy_image(self, src):
        guide = self.section()
        guide['sections'][1]['images'] = [{'src': src, 'alt': 'Models', 'caption': 'The list.'}]
        return self.check(guide)[0]

    def test_images_must_exist_be_png_and_wide_enough(self):
        self.assertTrue(any('missing' in p for p in self.legacy_image('docs/user-guide/images/gone.png')))
        (self.images / 'small.png').write_bytes(png(300))
        self.assertTrue(any('px wide' in p for p in self.legacy_image('docs/user-guide/images/small.png')))
        (self.images / 'fake.png').write_text('not a png')
        self.assertTrue(any("isn't a PNG" in p for p in self.legacy_image('docs/user-guide/images/fake.png')))

    def test_a_detailed_page_names_its_screens(self):
        guide = self.section()
        guide['sections'][0]['blocks'][2]['screen'] = 'nowhere'
        self.assertTrue(any("'nowhere' isn't in docs/user-guide/screens.json" in p for p in self.check(guide)[0]))
        guide = self.section()
        image = guide['sections'][0]['blocks'][2]
        del image['screen']
        image['src'] = 'docs/user-guide/images/chat.png'
        self.assertTrue(any('hand-made' in p for p in self.check(guide)[0]))

    def test_leftover_screens_and_images_fail(self):
        self.screens(['chat', 'unused'])
        (self.images / 'old.png').write_bytes(png(1280))
        problems = self.check(self.section())[0]
        self.assertTrue(any("'unused' isn't shown" in p for p in problems), problems)
        self.assertTrue(any('old.png isn\'t shown' in p for p in problems), problems)

    def test_long_sentences_fail(self):
        long = ' '.join(['word'] * 50) + '.'
        guide = self.section(paragraphs=[long])
        self.assertTrue(any('50-word sentence' in p for p in self.check(guide)[0]))

    def test_example_ids_are_unique(self):
        code = {'type': 'code', 'language': 'sh', 'code': 'ls', 'test': 'same'}
        guide = self.section()
        guide['sections'][0]['blocks'] += [code, dict(code)]
        self.assertTrue(any("'same' is also in" in p for p in self.check(guide)[0]))

    def test_structural_errors_come_from_the_publisher(self):
        problems, _ = self.check({'schemaVersion': 1, 'sections': []})
        self.assertEqual(len(problems), 1)
        self.assertIn('guide.json', problems[0])


class PullRequestTests(unittest.TestCase):
    """A feature's changelog fragment needs a guide change, or the label."""

    def setUp(self):
        import subprocess
        self.run_git = lambda *args: subprocess.run(['git', *args], cwd=self.root, check=True, capture_output=True)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'changes/unreleased').mkdir(parents=True)
        (self.root / 'docs/user-guide').mkdir(parents=True)
        (self.root / 'docs/user-guide/guide.json').write_text('{}')
        self.run_git('init', '-q', '-b', 'main')
        self.run_git('-c', 'user.email=t@t', '-c', 'user.name=t', 'commit', '-q', '--allow-empty', '-m', 'base')
        self.run_git('add', '.')
        self.run_git('-c', 'user.email=t@t', '-c', 'user.name=t', 'commit', '-q', '-m', 'guide')
        self.base = 'main'
        self.run_git('checkout', '-q', '-b', 'feature')
        self.addCleanup(setattr, checker, 'ROOT', checker.ROOT)
        checker.ROOT = self.root

    def commit(self, path, content):
        (self.root / path).write_text(content)
        self.run_git('add', path)
        self.run_git('-c', 'user.email=t@t', '-c', 'user.name=t', 'commit', '-q', '-m', path)

    def test_fixed_only_needs_no_page(self):
        self.commit('changes/unreleased/fix.md', '### Fixed\n\n- A bug.\n')
        self.assertEqual(checker.added_features(self.base), [])

    def test_a_feature_needs_the_guide(self):
        self.commit('changes/unreleased/feature.md', '### Added\n\n- A feature.\n')
        self.assertEqual(checker.added_features(self.base), ['changes/unreleased/feature.md'])
        self.assertFalse(checker.guide_changed(self.base))
        self.commit('docs/user-guide/guide.json', '{"changed": true}')
        self.assertTrue(checker.guide_changed(self.base))


if __name__ == '__main__':
    unittest.main()
