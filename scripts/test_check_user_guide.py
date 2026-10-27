import importlib.util
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
        images = self.root / 'docs/user-guide/images'
        images.mkdir(parents=True)
        (images / 'chat.png').write_bytes(png(1280))
        (images / 'small.png').write_bytes(png(300))

    def section(self, **extra):
        section = {
            'id': 'chat', 'title': 'Chat', 'paragraphs': ['Ask a question and read the answer.'],
            'summary': 'Talk to the model on this computer.', 'kind': 'task',
            'blocks': [
                {'type': 'list', 'title': 'What you need', 'items': ['A model, from [Models](#models).']},
                {'type': 'steps', 'items': ['Open Chat.', 'Type a question.', 'Press Send.']},
                {'type': 'image', 'src': 'docs/user-guide/images/chat.png', 'alt': 'Chat', 'caption': 'An answer with its sources.'},
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
        untested = self.section(blocks=blocks + [{'type': 'code', 'language': 'sh', 'code': 'toskarctl ask hi'}])
        self.assertTrue(any('tested example' in p for p in self.check(untested)[0]))
        tested = self.section(blocks=blocks + [{'type': 'code', 'language': 'sh', 'code': 'toskarctl ask hi', 'test': 'ask'}])
        self.assertEqual(self.check(tested)[0], [])

    def test_images_must_exist_be_png_and_wide_enough(self):
        for src, error in (('docs/user-guide/images/gone.png', 'missing'), ('docs/user-guide/images/small.png', 'px wide')):
            guide = self.section()
            guide['sections'][0]['blocks'][2]['src'] = src
            problems, _ = self.check(guide)
            self.assertTrue(any(error in p for p in problems), problems)
        (self.root / 'docs/user-guide/images/fake.png').write_text('not a png')
        guide = self.section()
        guide['sections'][0]['blocks'][2]['src'] = 'docs/user-guide/images/fake.png'
        self.assertTrue(any("isn't a PNG" in p for p in self.check(guide)[0]))

    def test_long_sentences_fail(self):
        long = ' '.join(['word'] * 50) + '.'
        guide = self.section(paragraphs=[long])
        self.assertTrue(any('50-word sentence' in p for p in self.check(guide)[0]))

    def test_example_ids_are_unique(self):
        code = {'type': 'code', 'language': 'sh', 'code': 'ls', 'test': 'same'}
        guide = self.section()
        guide['sections'][0]['blocks'] += [code, dict(code)]
        self.assertTrue(any("'same' is also in" in p for p in self.check(guide)[0]))

    def test_a_tested_example_is_in_a_language_ci_runs(self):
        guide = self.section()
        guide['sections'][0]['blocks'].append({'type': 'code', 'language': 'json', 'code': '{}', 'test': 'shape'})
        self.assertTrue(any("'shape' is json" in p for p in self.check(guide)[0]))

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
