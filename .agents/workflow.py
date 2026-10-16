#!/usr/bin/env python3
"""Portable workflow installer; Python 3.9+, standard library only."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

HERE = Path(__file__).resolve().parent
STATE = '.agents/workflow-install.json'
START = '<!-- project-workflow:start -->'
END = '<!-- project-workflow:end -->'
BLOCK = '''<!-- project-workflow:start -->
## 能力核对与决策笔记

按任务范围从 docs/FEATURE_MAP.md 核对现有入口、权限、接口和数据来源，再设计；优先复用已有能力。
非平凡改动遵循 .agents/skills/write-notes-like-deepseek/SKILL.md；先搜索活跃笔记，已有归属优先更新，真实取舍才新建。
提案先 proposed，实施验证后转 implemented，与代码一起提交；机械改动免写。用户和本项目规则优先于 Skill 的默认交互流程，已有授权内自主推进。
完成时同步能力地图与相关笔记，执行 python3 .agents/workflow.py verify 和改动相关的项目检查。结构检查不代替业务验收。
提交、发布、部署遵循本项目原有授权。说明见 docs/AGENT_NOTES.md。
<!-- project-workflow:end -->
'''


def digest(data):
    return hashlib.sha256(data).hexdigest()


def safe(root, rel):
    p = Path(rel)
    if p.is_absolute() or '..' in p.parts:
        raise ValueError('非法路径: ' + rel)
    target = root / p
    for part in [target, *target.parents]:
        if part == root:
            break
        if part.is_symlink():
            raise ValueError('不写入符号链接: ' + rel)
    return target


def payload():
    bundled = HERE / 'payload'
    if bundled.is_dir():
        return bundled
    return HERE.parents[1] / '.agents' / 'skills'


def install(args):
    root = Path(args.target).expanduser().resolve()
    if not root.is_dir():
        raise ValueError('目标项目目录不存在')
    state_path = safe(root, STATE)
    previous = json.loads(state_path.read_text(encoding='utf-8')) if state_path.exists() else {}
    old = previous.get('files', {})
    files = {}
    skill = payload() / 'write-notes-like-deepseek'
    if not skill.is_dir():
        raise ValueError('缺少 payload，请使用完整 ZIP 包')
    for p in sorted(skill.rglob('*')):
        if p.is_file():
            files['.agents/skills/write-notes-like-deepseek/' + p.relative_to(skill).as_posix()] = p.read_bytes()
    files['.agents/workflow.py'] = Path(__file__).read_bytes()
    templates = HERE / 'templates'
    # User-owned documents are created once and never managed by upgrades.
    seeds = {'docs/FEATURE_MAP.md': 'FEATURE_MAP.md', 'docs/AGENT_NOTES.md': 'AGENT_NOTES.md'}
    if args.ci:
        files['.github/workflows/agent-workflow.yml'] = (templates / 'ci.yml').read_bytes()
    writes, conflicts = {}, []
    for rel, data in files.items():
        path = safe(root, rel)
        if path.exists() and path.read_bytes() != data and digest(path.read_bytes()) != old.get(rel):
            conflicts.append(rel)
        elif not path.exists() or path.read_bytes() != data:
            writes[rel] = data
    for rel, template in seeds.items():
        if not safe(root, rel).exists():
            writes[rel] = (templates / template).read_bytes()
    agents = safe(root, 'AGENTS.md')
    raw = agents.read_bytes() if agents.exists() else b''
    content = raw.decode('utf-8')
    if START in content or END in content:
        if content.count(START) != 1 or content.count(END) != 1 or content.index(END) < content.index(START):
            conflicts.append('AGENTS.md (标记不完整)')
        else:
            current = content[content.index(START):content.index(END) + len(END)]
            if current != BLOCK.rstrip() and digest(current.encode()) != previous.get('block'):
                conflicts.append('AGENTS.md (托管规则已修改)')
            elif current != BLOCK.rstrip():
                writes['AGENTS.md'] = content.replace(current, BLOCK.rstrip()).encode()
    else:
        writes['AGENTS.md'] = raw + (b'\n\n' if raw else b'') + BLOCK.encode()
    # Preserve hashes for optional files installed earlier, even without --ci.
    hashes = dict(old)
    hashes.update({rel: digest(data) for rel, data in files.items()})
    state = {'version': (HERE / 'VERSION').read_text().strip(), 'files': hashes,
             'block': digest(BLOCK.rstrip().encode())}
    encoded = (json.dumps(state, ensure_ascii=False, indent=2) + '\n').encode()
    if not state_path.exists() or state_path.read_bytes() != encoded:
        writes[STATE] = encoded
    for rel in sorted(writes):
        print(('写入 ' if args.apply else '计划 ') + rel)
    if conflicts:
        raise ValueError('冲突，未写入任何文件；请先人工合并:\n' + '\n'.join(conflicts))
    if args.apply:
        for rel, data in writes.items():
            dest = safe(root, rel)
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(data)
        print('安装完成；请核对能力地图并执行 verify。')
    else:
        print('预览完成；加 --apply 才会写入。')


def check(args):
    root = Path(args.target).expanduser().resolve()
    state = json.loads(safe(root, STATE).read_text(encoding='utf-8'))
    changed = [rel for rel, expected in state['files'].items()
               if not safe(root, rel).is_file() or digest(safe(root, rel).read_bytes()) != expected]
    if changed:
        raise ValueError('托管文件缺失或已修改:\n' + '\n'.join(changed))
    rules = safe(root, 'AGENTS.md').read_text(encoding='utf-8')
    if rules.count(START) != 1 or rules.count(END) != 1 or rules.index(END) < rules.index(START):
        raise ValueError('AGENTS.md 托管规则缺失或标记不完整')
    block = rules[rules.index(START):rules.index(END) + len(END)]
    if digest(block.encode()) != state['block']:
        raise ValueError('AGENTS.md 托管规则已修改，请审阅并同步安装清单')
    bun = shutil.which('bun')
    if not bun:
        raise ValueError('未找到 Bun；请安装 Bun 并加入 PATH 后重试')
    print('工具包 ' + state['version'] + ' 完整；Bun: ' + bun, flush=True)
    if args.command == 'verify':
        env = os.environ.copy()
        env.pop('AGENT_NOTE_ROOT', None)
        for name in ['verify-agent-note-tree', 'verify-agent-note-format', 'verify-archived-agent-notes']:
            subprocess.run([bun, str(root / '.agents/skills/write-notes-like-deepseek/scripts' / (name + '.ts'))], cwd=root, env=env, check=True)


def main():
    # Redirected Windows streams may otherwise use a legacy code page.
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, 'reconfigure'):
            stream.reconfigure(encoding='utf-8')
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    inst = commands.add_parser('install')
    inst.add_argument('target')
    inst.add_argument('--apply', action='store_true')
    inst.add_argument('--ci', action='store_true')
    for name in ['doctor', 'verify']:
        sub = commands.add_parser(name)
        sub.add_argument('target', nargs='?', default='.')
    args = parser.parse_args()
    try:
        install(args) if args.command == 'install' else check(args)
    except (ValueError, OSError, subprocess.CalledProcessError) as exc:
        print(str(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
