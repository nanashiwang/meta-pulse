#!/usr/bin/env python3
"""Complete recovery sets and isolated restore drills; never restores over live DBs."""
import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from urllib.parse import parse_qs, unquote

ROOT = Path(__file__).resolve().parents[1]
COMPOSE = ['bash', str(ROOT/'deploy/compose.sh')]

class Failure(Exception): pass

def run(args, **kw):
    result = subprocess.run(args, capture_output=True, **kw)
    if result.returncode:
        raise Failure('command failed: '+Path(args[0]).name+' (details suppressed to protect credentials)')
    return result.stdout

def compose(*args): return run(COMPOSE+list(args)).decode().strip()

def envfile(path):
    values = {}
    for line in Path(path).read_text().splitlines():
        if not line.strip() or line.lstrip().startswith('#'): continue
        key, sep, value = line.partition('=')
        if not sep or not re.fullmatch(r'[A-Z][A-Z0-9_]*', key): raise Failure('invalid environment file')
        # Same literal KEY=value contract as deploy/lib.sh; never source shell code.
        values[key] = value
    return values

def settings():
    path = Path(os.environ.get('META_PULSE_ENV_FILE',ROOT/'.env'))
    values = envfile(path)
    model = json.loads(compose('--profile','maintenance','config','--format','json'))
    # Compose resolves $$ and substitutions once, exactly as for application DSNs.
    values.update({k:v or '' for k,v in model['services']['database-client'].get('environment',{}).items()})
    return path, values, model

def parse_dsn(raw):
    match = re.fullmatch(r'([^:]*)(?::(.*))?@tcp\(([^)]+)\)/([^?]+)(?:\?(.*))?',raw)
    if not match: raise Failure('backup requires a TCP MySQL DSN')
    user, password, address, database, query = match.groups()
    database = unquote(database)
    if not re.fullmatch(r'[\w-]+', database): raise Failure('unsupported database name')
    host, sep, port = address.rpartition(':')
    if not sep: host, port = address, '3306'
    host = host.strip('[]')
    if not host or not port.isdigit() or not 0<int(port)<65536: raise Failure('invalid MySQL endpoint')
    params = parse_qs(query or '',keep_blank_values=True)
    tls = params.get('tls',['false'])
    if len(tls)!=1 or tls[0] not in ('false','true'): raise Failure('backup TLS must be false or verified tls=true')
    for value in (user,password or '',host,database):
        if any(ord(c)<32 for c in value): raise Failure('control character in database configuration')
    return dict(user=user,password=password or '',host=host,port=port,database=database,tls=tls[0])

def option_file(cfg, ca=''):
    def quote(s): return '"'+s.replace('\\','\\\\').replace('"','\\"')+'"'
    lines = ['[client]', 'protocol=tcp']
    for key in ('user','password','host','port'): lines.append(key+'='+quote(cfg[key]))
    lines.append('ssl-mode='+('VERIFY_IDENTITY' if cfg['tls']=='true' else 'DISABLED'))
    if cfg['tls']=='false': lines.append('get-server-public-key')
    if ca and cfg['tls']=='true': lines.append('ssl-ca='+quote(ca))
    return ('\n'.join(lines)+'\n').encode()

def database_config(which, values, model):
    prefix, service, app, key = ('PULSE','mysql','pulse-api','PULSE_DB_DSN') if which=='pulse' else ('FORUM','forum-mysql','forum','FORUM_BINDING_GUARD_DSN')
    target = parse_dsn(model['services'][app]['environment'][key])
    if values.get(prefix+'_DB_MODE','local')=='local':
        local = model['services'].get(service,{}).get('environment',{})
        if target['host']!=service or target['port']!='3306' or target['database']!=local.get('MYSQL_DATABASE'):
            raise Failure('local database target differs from Compose service; configure external mode explicitly')
    backup = values.get(prefix+'_BACKUP_DB_DSN')
    if values.get(prefix+'_DB_MODE','local')=='external' and not backup:
        raise Failure(prefix+'_BACKUP_DB_DSN is required for external backups')
    if backup:
        cfg = parse_dsn(backup)
        if any(cfg[k]!=target[k] for k in ('host','port','database')):
            raise Failure('backup and application database targets differ')
        if target['tls']=='true' and cfg['tls']!='true': raise Failure('backup cannot downgrade application TLS')
    elif service in model['services']:
        cfg = dict(target,user='root',password=model['services'][service]['environment']['MYSQL_ROOT_PASSWORD'])
    else: raise Failure('local database service is missing')
    return cfg

def dump(which, output):
    _, values, model = settings()
    cfg = database_config(which,values,model)
    output = Path(output)
    if output.exists(): raise Failure('backup output already exists')
    output.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
    fd,name = tempfile.mkstemp(prefix='.dump-',dir=output.parent)
    try:
        with os.fdopen(fd,'wb') as dest, tempfile.TemporaryFile() as err:
            command = 'umask 077; cfg=$(mktemp); cat > "$cfg"; exec timeout 1800 mysqldump --defaults-extra-file="$cfg" --single-transaction --no-tablespaces --set-gtid-purged=OFF --routines --events --triggers --hex-blob '+shlex.quote(cfg['database'])
            proc = subprocess.Popen(COMPOSE+['run','--rm','--no-deps','-T','database-client','-c',command],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=err)
            try:
                proc.stdin.write(option_file(cfg,values.get('SSL_CERT_FILE',''))); proc.stdin.close()
                shutil.copyfileobj(proc.stdout,dest)
                if proc.wait()!=0:
                    err.seek(0)
                    error_text=err.read().decode(errors='replace')
                    code=re.search(r'(?:error: |ERROR )(\d+)',error_text,re.I)
                    diagnostic=' MySQL error '+code.group(1) if code else ''
                    raise Failure(which+' database dump failed;'+diagnostic+' no completed backup created')
            finally:
                proc.stdout.close()
                if proc.poll() is None: proc.kill(); proc.wait()
            dest.flush(); os.fsync(dest.fileno())
        if Path(name).stat().st_size==0: raise Failure('empty database dump')
        os.link(name,output) # do not overwrite a racing destination
    finally: Path(name).unlink(missing_ok=True)
    return cfg['database']

def copy_volume(service, path, output, required=True):
    container = compose('ps','-a','-q',service).splitlines()
    if not container:
        if required: raise Failure(service+' container missing; recovery set would be incomplete')
        return
    output.mkdir(parents=True,mode=0o700)
    run(['docker','cp',container[0]+':'+path+'/.',str(output)])

def hashes(root):
    result={}
    for path in sorted(root.rglob('*')):
        if path.is_symlink(): raise Failure('symlinks are not supported in recovery sets')
        if path.stat().st_dev != root.stat().st_dev: raise Failure('cross-device file in recovery set')
        if path.is_dir(): continue
        if path.stat().st_nlink != 1: raise Failure('hard-linked file in recovery set')
        if not path.is_file(): raise Failure('special file in recovery set')
        if path.name=='manifest.json' and path.parent==root: continue
        with path.open('rb') as f:
            digest=hashlib.sha256()
            while chunk:=f.read(1024*1024): digest.update(chunk)
        result[str(path.relative_to(root))]=digest.hexdigest()
    return result

def verify(root):
    root=Path(root)
    if root.is_symlink(): raise Failure('backup directory may not be a symlink')
    manifest=json.loads((root/'manifest.json').read_text())
    if manifest.get('format')!=1 or manifest.get('files')!=hashes(root): raise Failure('recovery set checksum mismatch')
    databases=manifest.get('databases')
    if not isinstance(databases,dict) or set(databases)!={'pulse','forum'} or any(not isinstance(db,str) or not re.fullmatch(r'[\w-]+',db) for db in databases.values()):
        raise Failure('invalid recovery database inventory')
    if manifest.get('consistency') not in ('quiesced','online-per-database'):
        raise Failure('invalid recovery consistency marker')
    required=['pulse.sql','forum.sql','.env','compose.json']
    if any(k not in manifest['files'] for k in required): raise Failure('incomplete recovery set')
    for directory in ('runtime-keys/api/','runtime-keys/worker/','forum-data/'):
        if not any(k.startswith(directory) for k in manifest['files']): raise Failure('missing recovery material: '+directory)
    return manifest

@contextlib.contextmanager
def lock():
    gitdir=Path(run(['git','-C',str(ROOT),'rev-parse','--absolute-git-dir']).decode().strip())
    with (gitdir/'meta-pulse-update.lock').open('a') as f:
        try: fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)
        except BlockingIOError: raise Failure('deployment/backup already in progress')
        yield

def create(quiesced=False):
    env_path, values, model=settings()
    # Validate both targets before any pause or snapshot creation.
    for which in ('pulse','forum'): database_config(which,values,model)
    parent=ROOT/'.data/recovery-sets';parent.mkdir(parents=True,exist_ok=True,mode=0o700)
    root=parent/(time.strftime('%Y%m%dT%H%M%SZ',time.gmtime())+'-'+secrets.token_hex(4))
    root.mkdir(mode=0o700)
    restart=[]
    paused=False
    try:
        if quiesced:
            running=set(compose('ps','--status','running','--services').splitlines())
            restart=[s for s in ('pulse-api','pulse-worker','forum') if s in running]
            if restart:
                paused=True
                compose('stop','-t','30',*restart)
        shutil.copyfile(env_path,root/'.env');(root/'.env').chmod(0o600)
        (root/'compose.json').write_text(json.dumps(model));(root/'compose.json').chmod(0o600)
        dbs={which:dump(which,root/(which+'.sql')) for which in ('pulse','forum')}
        copy_volume('forum','/data',root/'forum-data')
        for role in ('api','worker'): copy_volume('pulse-'+role,'/app/runtime-keys',root/'runtime-keys'/role)
        # Docker cp can preserve world-readable modes; protect every artifact.
        for path in root.rglob('*'):
            if not path.is_symlink(): path.chmod(0o700 if path.is_dir() else 0o600)
        manifest={'format':1,'created_at':time.time(),'revision':run(['git','-C',str(ROOT),'rev-parse','HEAD']).decode().strip(),'completed':False,'consistency':'quiesced' if quiesced else 'online-per-database','databases':dbs,'files':hashes(root)}
        (root/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n');(root/'manifest.json').chmod(0o600)
        verify(root)
    finally:
        if paused: compose('start',*restart)
    return root

def offsite(root, config):
    verify(root)
    cfgpath=Path(config)
    if cfgpath.stat().st_mode & 0o077: raise Failure('offsite config must have mode 600')
    extra=envfile(cfgpath)
    if not extra.get('RESTIC_REPOSITORY') or not extra.get('RESTIC_PASSWORD_FILE'): raise Failure('restic repository and password file are required')
    if Path(extra['RESTIC_PASSWORD_FILE']).stat().st_mode & 0o077: raise Failure('restic password file must have mode 600')
    env={**os.environ,**extra}
    # Repository initialization is deliberate, outside scheduled backup runs.
    raw=run(['restic','backup','--json','--tag','meta-pulse',root.name],cwd=root.parent,env=env).decode()
    summaries=[json.loads(line) for line in raw.splitlines() if line.startswith('{')]
    snapshot=next((x.get('snapshot_id') for x in summaries if x.get('message_type')=='summary'),None)
    if not snapshot or not re.fullmatch('[a-f0-9]+',snapshot): raise Failure('restic snapshot was not confirmed')
    with tempfile.TemporaryDirectory(prefix='metar-offsite-verify-') as tmp:
        run(['restic','restore',snapshot,'--target',tmp],env=env)
        verify(Path(tmp)/root.name)
    return snapshot

def drill(root, image):
    manifest=verify(root)
    if manifest['consistency']!='quiesced': raise Failure('full recovery drill requires a quiesced recovery set')
    if len(set(manifest['databases'].values()))!=2:
        raise Failure('this single-instance drill requires distinct schema names; restore identical names in separate isolated instances')
    name='metar-restore-drill-'+secrets.token_hex(6)
    started=time.monotonic()
    # No network and no production mounts. Only MySQL is run: no Benefit or mail.
    run(['docker','run','-d','--name',name,'--network','none','-e','MYSQL_ROOT_PASSWORD=isolated_drill_only',image,'--log-bin-trust-function-creators=1'])
    def sql(statement):
        return run(['docker','exec',name,'sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot -N -e "$1"','sh',statement]).decode()
    try:
        for _ in range(90):
            try: sql('SELECT 1');break
            except Failure: time.sleep(1)
        else: raise Failure('isolated MySQL did not become ready')
        for which,db in manifest['databases'].items():
            if which not in ('pulse','forum') or not re.fullmatch(r'[\w-]+',db): raise Failure('invalid database in manifest')
            sql('CREATE DATABASE `'+db+'` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci')
            with (root/(which+'.sql')).open('rb') as f:
                run(['docker','exec','-i',name,'sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot "$1"','sh',db],stdin=f)
            counts=sql("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='"+db+"'").strip()
            if int(counts)==0: raise Failure('restored database is empty')
            # Recreate definer accounts only inside the disposable isolated instance.
            definers=sql("SELECT DISTINCT DEFINER FROM information_schema.triggers WHERE trigger_schema='"+db+"'").splitlines()
            for definer in definers:
                user,host=definer.rsplit('@',1)
                ident="'"+user.replace('\\','\\\\').replace("'","''")+"'@'"+host.replace('\\','\\\\').replace("'","''")+"'"
                sql('CREATE USER IF NOT EXISTS '+ident+' ACCOUNT LOCK; GRANT ALL ON `'+db+'`.* TO '+ident)
            if which=='pulse':
                query='''SELECT COUNT(*) FROM (
SELECT a.user_id FROM pulse_account a LEFT JOIN
(SELECT user_id,period_id,asset_type,SUM(amount) balance,COUNT(*) version FROM pulse_ledger_entry GROUP BY user_id,period_id,asset_type) l
USING(user_id,period_id,asset_type) WHERE a.balance<>COALESCE(l.balance,0) OR a.version<>COALESCE(l.version,0)
UNION ALL SELECT l.user_id FROM pulse_ledger_entry l LEFT JOIN pulse_account a USING(user_id,period_id,asset_type) WHERE a.id IS NULL
) mismatches'''
                mismatch=sql('USE `'+db+'`; '+query).strip()
                if int(mismatch)!=0: raise Failure('Pulse ledger/account reconciliation failed in restored snapshot')
            else:
                exists=sql("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='"+db+"' AND table_name='metar_exp_account'").strip()
                if int(exists):
                    mismatch=sql('USE `'+db+'''`; SELECT COUNT(*) FROM metar_exp_account a LEFT JOIN (SELECT user_id,SUM(delta) balance FROM metar_exp_ledger GROUP BY user_id) l USING(user_id) WHERE a.balance<>COALESCE(l.balance,0)''').strip()
                    if int(mismatch)!=0: raise Failure('Forum experience reconciliation failed in restored snapshot')
            print(which+': restored '+counts+' tables and reconciled accounts')
        print('SQL restore, local account reconciliation and artifact checks passed in %.1fs; external Benefit reconciliation is still required' % (time.monotonic()-started))
    finally: run(['docker','rm','-fv',name])

def prune_local(current, keep):
    if keep == 0: return
    candidates=[]
    for path in current.parent.iterdir():
        if path == current or path.is_symlink() or not path.is_dir() or (path/'.keep').exists(): continue
        if not re.fullmatch(r'[0-9]{8}T[0-9]{6}Z-[a-f0-9]{8}',path.name): continue
        try:
            manifest=verify(path)
            if manifest.get('completed') is True: candidates.append(path)
        except (Failure,OSError,ValueError): continue
    # Unknown, partial and failed-upload sets are retained. Current always wins.
    for path in sorted(candidates,reverse=True)[max(0,keep-1):]:
        shutil.rmtree(path)


def main():
    os.umask(0o077)
    p=argparse.ArgumentParser(description=__doc__);sub=p.add_subparsers(dest='command',required=True)
    d=sub.add_parser('dump');d.add_argument('--database',choices=['pulse','forum'],required=True);d.add_argument('--output',type=Path,required=True)
    c=sub.add_parser('create');c.add_argument('--quiesced',action='store_true');c.add_argument('--offsite-config',type=Path);c.add_argument('--keep-local',type=int,default=3)
    for key in ('verify','upload','restore-drill'):
        s=sub.add_parser(key);s.add_argument('directory',type=Path)
        if key=='upload':s.add_argument('--offsite-config',type=Path,required=True)
        if key=='restore-drill':s.add_argument('--image',default='mysql:8.4')
    a=p.parse_args()
    if a.command=='dump': dump(a.database,a.output)
    elif a.command=='verify':verify(a.directory);print('Recovery set verified')
    elif a.command=='upload':
        with lock(): print('Remote snapshot verified: '+offsite(a.directory.resolve(),a.offsite_config))
    elif a.command=='restore-drill':
        with lock(): drill(a.directory.resolve(),a.image)
    elif a.command=='create':
        if a.keep_local < 0: raise Failure('keep-local must be non-negative')
        with lock():
            root=create(a.quiesced)
            if a.offsite_config:print('Remote snapshot verified: '+offsite(root,a.offsite_config))
            manifest=verify(root);manifest['completed']=True
            (root/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
            prune_local(root,a.keep_local)
            print(root)

if __name__=='__main__':
    def terminate(signum, frame): raise Failure('backup interrupted; attempting to resume paused containers')
    signal.signal(signal.SIGTERM,terminate)
    signal.signal(signal.SIGINT,terminate)
    try: main()
    except (Failure,OSError,ValueError,KeyError) as error:
        # Do not print driver/CLI/config errors, which may contain secrets.
        print('Backup operation failed: '+(str(error) if isinstance(error,Failure) else type(error).__name__),file=sys.stderr)
        sys.exit(1)
