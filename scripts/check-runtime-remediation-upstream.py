#!/usr/bin/env python3
"""Daily retirement check. Reads public upstream state; never updates the policy."""
import datetime
import io
import json
import pathlib
import tarfile
import urllib.request

def fetch(url, limit):
    request = urllib.request.Request(url, headers={'User-Agent': 'Aeontra-runtime-retirement-watch'})
    with urllib.request.urlopen(request, timeout=30) as response:
        result = response.read(limit+1)
    if len(result) > limit:
        raise ValueError('upstream response exceeds its budget')
    return result

def check(policy, now, get=fetch):
    expires = datetime.datetime.fromisoformat(policy['expires_at'].replace('Z', '+00:00'))
    if now >= expires:
        raise ValueError('Temporary runtime remediation expired; replace it with official packages. No automatic renewal.')
    pr = json.loads(get('https://api.github.com/repos/wolfi-dev/os/pulls/78784', 1024*1024))
    issue = json.loads(get('https://api.github.com/repos/anchore/grype/issues/3884', 1024*1024))
    if type(pr.get('merged')) is not bool or issue.get('state') not in ('open', 'closed'):
        raise ValueError('upstream state could not be verified')
    index = get('https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz', 16*1024*1024)
    with tarfile.open(fileobj=io.BytesIO(index), mode='r:gz') as archive:
        with archive.extractfile('APKINDEX') as f:
            data = f.read(128*1024*1024+1)
        if len(data) > 128*1024*1024:
            raise ValueError('upstream index exceeds its budget')
    available, found = False, False
    for block in data.decode().split('\n\n'):
        fields = dict(line.split(':', 1) for line in block.splitlines() if ':' in line)
        if fields.get('P') == 'glibc-2.44':
            found = True
            version = fields.get('V', '')
            if version.startswith('2.44-r') and version[6:].isdigit() and int(version[6:]) >= 14:
                available = True
    if not found:
        raise ValueError('official glibc package disappeared from the index; review replacement')
    if pr.get('merged') and available:
        raise ValueError('Official glibc backport is published: retire the custom build and pin/verify the official APK family.')
    if issue.get('state') == 'closed':
        raise ValueError('SQLite applicability issue changed: review the corrected feed and retire the temporary classification.')
    return 'runtime_remediation_upstream=pending expires='+policy['expires_at']


if __name__ == '__main__':
    policy = json.loads(pathlib.Path('security/runtime-remediation.json').read_text())
    print(check(policy, datetime.datetime.now(datetime.timezone.utc)))
