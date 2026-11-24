# Read-only Linux process sampling. No credentials or process environment are read.
import json
import os
import sys
import time

expected = sys.argv[1] if len(sys.argv) > 1 else ""
candidates = []
all_processes = []
for pid in os.listdir('/proc'):
    if not pid.isdigit():
        continue
    try:
        root = '/proc/' + pid + '/'
        args = open(root + 'cmdline', 'rb').read().split(b'\0')
        if not any(os.path.basename(a) == b'barman-cloud-backup' for a in args):
            continue
        stat = open(root + 'stat').read().rsplit(') ', 1)[1].split()
        info = (int(pid), int(stat[1]), stat)
        all_processes.append(info)
        if expected != "":
            if b'--name' not in args or args[args.index(b'--name') + 1].decode() != expected:
                continue
        candidates.append(info)
    except (OSError, ValueError, IndexError):
        continue
parents = [p for p in candidates if p[1] not in {x[0] for x in candidates}]
if len(parents) != 1:
    # Fallback: status.backupName may be empty and the old
    # reconciliationStartedAt guess can be off by a second. If there is
    # exactly one backup process running, use it.
    all_parents = [p for p in all_processes if p[1] not in {x[0] for x in all_processes}]
    if len(parents) == 0 and len(all_parents) == 1:
        parents = all_parents
if len(parents) != 1:
    print(json.dumps({'error': 'No unique matching backup process; it may be starting, finishing, or unavailable.'}))
    sys.exit(0)
pid, _, stat = parents[0]
try:
    io = dict((k, int(v)) for k, v in (line.split(':') for line in open('/proc/%s/io' % pid)))
    print(json.dumps({'identity': '%s:%s' % (pid, stat[19]), 'read_bytes': io['rchar'],
                      'cpu_seconds': (int(stat[11]) + int(stat[12])) / os.sysconf('SC_CLK_TCK'),
                      'sample_seconds': time.monotonic()}))
except OSError:
    print(json.dumps({'error': 'Backup process exited during sampling.'}))
