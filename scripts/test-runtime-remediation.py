#!/usr/bin/env python3
"""Offline controls for final-image inspection and automatic patch retirement."""
import datetime
import hashlib
import importlib.util
import io
import json
import pathlib
import tarfile
import unittest


def module(name):
    spec = importlib.util.spec_from_file_location(name, pathlib.Path(__file__).with_name(name+'.py'))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


proof = module('verify-runtime-remediation')
watch = module('check-runtime-remediation-upstream')


def archive(files, zipped=False):
    result = io.BytesIO()
    with tarfile.open(fileobj=result, mode='w:gz' if zipped else 'w') as tar:
        for name, body in files:
            entry = tarfile.TarInfo(name)
            entry.size = len(body)
            tar.addfile(entry, io.BytesIO(body))
    result.seek(0)
    return result


class ImageProofTests(unittest.TestCase):
    def test_regular_files_are_all_hashed(self):
        _, hashes, _ = proof.inspect_archive(archive([('usr/lib/data', b'benign')]), 'not-a-file-hash')
        self.assertEqual(hashes, {'usr/lib/data': hashlib.sha256(b'benign').hexdigest()})

    def test_cli_detected_with_changed_name_mode_and_hash(self):
        body = b'prefix SQLite version %s payload .recover suffix'
        for name in ('usr/bin/sqlite3', 'usr/lib/renamed.bin'):
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, 'shell component found'):
                proof.inspect_archive(archive([(name, body)]), 'different-control-hash')
        proof.verify_renamed_control(body)

    def test_signature_across_stream_boundary(self):
        body = b'x'*(1024*1024-4)+b'SQLite version %s'+b'y'*100+b'.recover '
        with self.assertRaisesRegex(ValueError, 'shell component found'):
            proof.inspect_archive(archive([('renamed', body)]), 'different')

    def test_path_escape_and_duplicate_are_rejected(self):
        for files in ([('../escape', b'x')], [('same', b'a'), ('same', b'b')]):
            with self.assertRaisesRegex(ValueError, 'invalid or duplicate'):
                proof.inspect_archive(archive(files), 'different')

    def test_symlink_resolution_and_cycles(self):
        link = tarfile.TarInfo('lib64')
        link.type, link.linkname = tarfile.SYMTYPE, 'usr/lib'
        self.assertEqual(proof.resolved_hash('/lib64/loader', {'lib64': link}, {'usr/lib/loader': 'expected'}), 'expected')
        link.linkname = 'lib64'
        with self.assertRaisesRegex(ValueError, 'link cycle'):
            proof.resolved_hash('/lib64/loader', {'lib64': link}, {})


class RetirementTests(unittest.TestCase):
    now = datetime.datetime(2026, 11, 7, tzinfo=datetime.timezone.utc)
    policy = {'expires_at': '2026-11-21T00:00:00Z'}

    def fixture(self, merged=False, version='2.44-r13', closed=False):
        def get(url, limit):
            if '/pulls/' in url:
                return json.dumps({'merged': merged}).encode()
            if '/issues/' in url:
                return json.dumps({'state': 'closed' if closed else 'open'}).encode()
            return archive([('APKINDEX', ('P:glibc-2.44\nV:'+version+'\n').encode())], True).getvalue()
        return get

    def test_pending_requires_no_change(self):
        self.assertIn('pending', watch.check(self.policy, self.now, self.fixture()))
        self.assertIn('pending', watch.check(self.policy, self.now, self.fixture(merged=True)))

    def test_official_backport_retires_maintenance(self):
        for version in ('2.44-r14', '2.44-r15'):
            with self.subTest(version=version), self.assertRaisesRegex(ValueError, 'Official glibc backport'):
                watch.check(self.policy, self.now, self.fixture(merged=True, version=version))

    def test_feed_change_and_expiry_require_review(self):
        with self.assertRaisesRegex(ValueError, 'SQLite applicability'):
            watch.check(self.policy, self.now, self.fixture(closed=True))
        def no_network(*args):
            self.fail('expired policy must fail before any network access')
        with self.assertRaisesRegex(ValueError, 'expired'):
            watch.check(self.policy, self.now+datetime.timedelta(days=14), no_network)

    def test_network_failure_is_not_a_green_result(self):
        def unavailable(*args):
            raise OSError('unavailable')
        with self.assertRaises(OSError):
            watch.check(self.policy, self.now, unavailable)

    def test_missing_state_cannot_be_reported_pending(self):
        with self.assertRaisesRegex(ValueError, 'could not be verified'):
            watch.check(self.policy, self.now, lambda *args: b'{}')


if __name__ == '__main__':
    unittest.main()
