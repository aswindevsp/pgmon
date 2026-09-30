# Read-only Linux restore sampling. No credentials or process environment are read.
import json
import os
import re
import time

PGDATA = '/var/lib/postgresql/data/pgdata'
WAL_NAME = re.compile(r'^[0-9A-F]{24}$')
BACKUP_ID = re.compile(r'^\d{8}T\d{6}$')
START_WAL = re.compile(r'file ([0-9A-F]{24})')


def basename(arg):
    return os.path.basename(arg)


def processes():
    found = {'restore': [], 'wal': [], 'tar': []}
    for pid in os.listdir('/proc'):
        if not pid.isdigit():
            continue
        try:
            args = open('/proc/%s/cmdline' % pid, 'rb').read().split(b'\0')
            names = [basename(a) for a in args if a]
            stat = open('/proc/%s/stat' % pid).read().rsplit(') ', 1)[1].split()
            info = (int(pid), int(stat[1]), stat, args)
            if b'barman-cloud-wal-restore' in names:
                found['wal'].append(info)
            elif b'barman-cloud-restore' in names:
                found['restore'].append(info)
            elif names and names[0] == b'tar' and b'--extract' in args:
                found['tar'].append(info)
        except (OSError, ValueError, IndexError):
            continue
    return found


def parents(items):
    pids = {p[0] for p in items}
    return [p for p in items if p[1] not in pids]


def io_counters(pid):
    values = {}
    for line in open('/proc/%s/io' % pid):
        key, value = line.split(':')
        values[key] = int(value)
    return values


def cpu_seconds(stat):
    return (int(stat[11]) + int(stat[12])) / os.sysconf('SC_CLK_TCK')


def pick(items):
    chosen = parents(items) or items
    if not chosen:
        return None
    return chosen[0]


def backup_id(args):
    for arg in args:
        text = arg.decode('ascii', 'replace')
        if BACKUP_ID.match(text):
            return text
    return ''


def manifest_total():
    path = os.path.join(PGDATA, 'backup_manifest')
    try:
        raw = open(path, 'rb').read()
    except OSError:
        return 0
    if b'"Manifest-Checksum"' not in raw:
        return 0
    try:
        data = json.loads(raw)
    except ValueError:
        return 0
    total = 0
    for entry in data.get('Files') or []:
        try:
            total += int(entry.get('Size') or 0)
        except (TypeError, ValueError):
            continue
    return total


def start_wal():
    for name in ('backup_label', 'backup_label.old'):
        try:
            text = open(os.path.join(PGDATA, name), 'r', errors='replace').read()
        except OSError:
            continue
        match = START_WAL.search(text)
        if match:
            return match.group(1)
    return ''


def wal_listing():
    try:
        names = os.listdir(os.path.join(PGDATA, 'pg_wal'))
    except OSError:
        return 0, ''
    files = [name for name in names if WAL_NAME.match(name)]
    files.sort()
    return len(files), (files[-1] if files else '')


found = processes()
restore = pick(found['restore'])
tar = pick(found['tar'])
wal = pick(found['wal'])
wal_files, last_wal = wal_listing()
total_bytes = manifest_total()
if restore is not None or tar is not None:
    phase = 'base'
elif wal is not None or wal_files > 0:
    phase = 'wal'
elif total_bytes > 0:
    phase = 'wal'
else:
    phase = 'waiting'

sample = {
    'phase': phase,
    'identity': '',
    'written_bytes': 0,
    'total_bytes': total_bytes,
    'cpu_seconds': 0.0,
    'sample_seconds': time.monotonic(),
    'backup_id': backup_id(restore[3]) if restore else '',
    'wal_files': wal_files,
    'last_wal': last_wal,
    'start_wal': start_wal(),
    'error': '',
}

try:
    if tar is not None:
        counters = io_counters(tar[0])
        sample['identity'] = '%s:%s' % (tar[0], tar[2][19])
        sample['written_bytes'] = counters.get('write_bytes', 0)
        sample['cpu_seconds'] = cpu_seconds(tar[2])
    elif restore is not None:
        counters = io_counters(restore[0])
        sample['identity'] = '%s:%s' % (restore[0], restore[2][19])
        sample['written_bytes'] = counters.get('wchar', 0)
        sample['cpu_seconds'] = cpu_seconds(restore[2])
    elif wal is not None:
        counters = io_counters(wal[0])
        sample['identity'] = '%s:%s' % (wal[0], wal[2][19])
        sample['written_bytes'] = counters.get('rchar', 0)
        sample['cpu_seconds'] = cpu_seconds(wal[2])
        for arg in wal[3]:
            text = arg.decode('ascii', 'replace')
            if WAL_NAME.match(text):
                sample['last_wal'] = text
except OSError:
    sample['error'] = 'Restore process exited during sampling'

print(json.dumps(sample))
