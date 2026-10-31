#!/usr/bin/env python3
"""Isolated Docker/browser integration checks; never touches production containers.
Run after building local/dqgod-mymihomo:dev. Requires Python Playwright + Chromium.
"""
import http.cookiejar
import http.server
import json
import os
import re
from pathlib import Path
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
BASE = 'http://127.0.0.1:19098'
NAME = 'mymihomo-integration-check'
PASSWORD = 'integration-only-password'
YAML = "proxies: []\nproxy-groups:\n  - name: Test\n    type: select\n    proxies: [DIRECT, REJECT]\nrules: ['MATCH,Test']\n"
requests_seen = []


def docker(*args):
    return subprocess.check_output(['docker', *args], text=True).strip()


class Fixture(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        requests_seen.append(self.path)
        self.send_response(200)
        self.send_header('Subscription-Userinfo', 'upload=1024; download=1073741824; total=53687091200; expire=1900000000')
        self.send_header('Content-Disposition', 'attachment; filename="Demo.yaml"')
        self.end_headers()
        self.wfile.write((YAML if self.path != '/invalid' else 'rules: [').encode())

    def log_message(self, *_):
        pass


def client():
    return urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))


session = client()


def request(path, method='GET', body=None, expected=200):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(BASE + path, data=data, method=method, headers={'Content-Type': 'application/json'})
    try:
        response = session.open(req, timeout=60)
    except urllib.error.HTTPError as e:
        response = e
    raw = response.read()
    assert response.status == expected, (path, response.status, raw[:400])
    if response.headers.get('Content-Type', '').startswith('application/json'):
        return json.loads(raw)
    return raw


def login():
    session.open(urllib.request.Request(BASE + '/auth/login', data=urllib.parse.urlencode({'password': PASSWORD}).encode()), timeout=20).read()


def wait_ready():
    for _ in range(60):
        try:
            request('/healthz')
            return
        except (OSError, AssertionError):
            time.sleep(1)
    raise RuntimeError('test gateway did not start')


def browser_checks():
    from playwright.sync_api import sync_playwright, expect
    with sync_playwright() as pw:
        browser = pw.chromium.launch(headless=True, args=['--no-sandbox', '--disable-dev-shm-usage'])
        context = browser.new_context(viewport={'width': 1440, 'height': 1000}, locale='zh-CN')
        page = context.new_page()
        errors = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.goto(BASE + '/ui/zashboard/')
        page.get_by_label('访问密码').fill(PASSWORD)
        page.get_by_role('button', name='登录', exact=True).click()
        page.wait_for_url('**/ui/zashboard/**')
        page.goto(BASE + '/ui/zashboard/#/subscriptions')
        page.get_by_role('heading', name='订阅', exact=True).wait_for(timeout=45000)
        page.get_by_text('全局扩展脚本', exact=True).wait_for()
        page.wait_for_timeout(2500)
        assert page.locator('article').count() >= 2
        # Verify WebSocket proxy and cookie authentication using the real core.
        status = page.evaluate('''() => new Promise(resolve => {
          const ws = new WebSocket(location.origin.replace('http', 'ws') + '/traffic');
          const timer = setTimeout(() => { ws.close(); resolve(false) }, 10000);
          ws.onmessage = () => { clearTimeout(timer); ws.close(); resolve(true) };
          ws.onerror = () => { clearTimeout(timer); resolve(false) };
        })''')
        assert status, 'authenticated WebSocket failed'
        page.get_by_role('button', name='新建', exact=True).click()
        dialog = page.get_by_role('dialog')
        dialog.get_by_label('名称', exact=True).fill('浏览器本地配置')
        dialog.get_by_role('button', name='导入并校验').click()
        dialog.wait_for(state='hidden', timeout=30000)
        page.get_by_role('heading', name='浏览器本地配置', exact=True).wait_for()
        page.get_by_role('button', name='Demo 订阅 更多操作').click()
        page.get_by_role('button', name='查看原始文件', exact=True).click()
        dialog = page.get_by_role('dialog')
        assert dialog.locator('textarea').get_attribute('readonly') is not None
        assert dialog.get_by_role('button', name='保存并应用', exact=True).count() == 0
        dialog.get_by_role('button', name='关闭', exact=True).first.click()
        page.get_by_role('button', name='查看运行时配置', exact=True).click()
        dialog = page.get_by_role('dialog')
        original = dialog.locator('textarea').input_value()
        assert 'script.example' in original and '[已隐藏]' in original
        dialog.locator('textarea').focus()
        page.keyboard.press('Control+f')
        field = dialog.get_by_role('searchbox', name='搜索当前配置')
        field.fill('DOMAIN')
        assert field.evaluate('(el) => document.activeElement === el')
        assert '0 / 0' not in dialog.get_by_role('status').inner_text()
        field.press('Enter')
        selected = dialog.locator('textarea').evaluate('(el) => el.value.slice(el.selectionStart, el.selectionEnd)')
        assert selected == 'DOMAIN'
        page.keyboard.press('Escape')
        assert dialog.get_by_role('search').count() == 0
        assert dialog.locator('textarea').input_value() == original
        dialog.get_by_role('button', name='关闭', exact=True).first.click()
        page.get_by_role('button', name='全局覆写配置', exact=False).click()
        page.get_by_role('textbox', name='全局覆写配置').fill('prepend-rules:\n  - DOMAIN,ui-test.example,DIRECT\n')
        page.get_by_role('button', name='保存并应用', exact=True).click()
        page.get_by_role('dialog').wait_for(state='hidden', timeout=30000)
        screenshots = ROOT / '.build/screenshots'
        screenshots.mkdir(exist_ok=True, parents=True)
        page.screenshot(path=str(screenshots / 'subscriptions-desktop.png'), full_page=True)
        page.set_viewport_size({'width': 390, 'height': 844})
        page.wait_for_timeout(500)
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), 'mobile overflow'
        page.screenshot(path=str(screenshots / 'subscriptions-mobile.png'), full_page=True)
        page.set_viewport_size({'width': 1440, 'height': 1000})
        def proxy_fixture(route):
            response = route.fetch()
            payload = response.json()
            if response.status != 200 or 'proxies' not in payload:
                route.fulfill(response=response)
                return
            nodes = [f'Node{i:03d}' for i in range(60)]
            for i, name in enumerate(nodes):
                payload['proxies'][name] = {'name': name, 'type': 'Http', 'udp': False, 'history': [{'time': '2026-10-31T00:00:00Z', 'delay': 0 if i == 10 else i + 1}]}
            payload['proxies']['Test']['all'] = nodes
            payload['proxies']['Test']['now'] = nodes[-1]
            route.fulfill(json=payload)
        page.route(re.compile(re.escape(BASE) + r'/proxies$'), proxy_fixture)
        page.route(re.compile(r'/proxies/[^/]+/delay'), lambda route: route.fulfill(json={'delay': 42}))
        page.goto(BASE + '/ui/zashboard/#/proxies')
        page.reload()
        group = page.locator('[data-group-name="Test"]').first
        group.wait_for()
        group.get_by_role('button', name='定位当前节点', exact=True).click()
        expect(group.locator('[data-proxy-name="Node059"]')).to_be_in_viewport(timeout=10000)
        group.get_by_role('combobox', name='本组节点排序').select_option('latencydesc')
        group.locator('.collapse-motion-content').evaluate('(el) => el.scrollTop = 0')
        page.wait_for_timeout(800)
        assert group.locator('[data-proxy-name]').first.get_attribute('data-proxy-name') == 'Node059'
        group.get_by_role('combobox', name='本组节点排序').select_option('latencyasc')
        page.wait_for_timeout(800)
        assert group.locator('[data-proxy-name]').first.get_attribute('data-proxy-name') == 'Node000'
        assert group.get_by_role('combobox', name='本组节点排序').input_value() == 'latencyasc'
        group.get_by_role('button', name='隐藏本组不可用节点').click()
        assert group.get_by_role('button', name='隐藏本组不可用节点').get_attribute('aria-pressed') == 'true'
        group.get_by_role('button', name='定位当前节点', exact=True).click()
        assert group.get_by_role('button', name='隐藏本组不可用节点').get_attribute('aria-pressed') == 'false'
        # Verify the visible test button reaches the group's core delay endpoint.
        with page.expect_request(lambda r: '/delay' in r.url, timeout=15000):
            group.get_by_role('button', name='测试本组延迟').click()
        expect(group.get_by_role('button', name='测试本组延迟')).to_be_enabled(timeout=30000)
        page.screenshot(path=str(screenshots / 'proxy-controls.png'), full_page=True)
        page.unroute_all(behavior='wait')
        page.goto(BASE + '/ui/zashboard/#/settings')
        page.get_by_role('button', name='退出登录', exact=True).click()
        page.wait_for_url('**/login')
        assert page.request.get(BASE + '/api/profiles').status == 401
        assert not errors, errors
        # Password changes are exercised only in this disposable test instance.
        page.get_by_label('访问密码').fill(PASSWORD)
        page.get_by_role('button', name='登录', exact=True).click()
        page.wait_for_url('**/ui/zashboard/**')
        page.goto(BASE + '/ui/zashboard/#/subscriptions')
        assert page.get_by_role('button', name='退出登录', exact=True).count() == 0
        page.goto(BASE + '/ui/zashboard/#/settings')
        page.get_by_role('button', name='修改访问密码', exact=True).click()
        panel = page.get_by_role('region', name='访问与安全')
        panel.get_by_label('当前密码', exact=True).fill('wrong-password')
        panel.get_by_label('新密码', exact=True).fill('integration-changed-password')
        panel.get_by_label('确认新密码', exact=True).fill('integration-changed-password')
        panel.get_by_role('button', name='保存新密码').click()
        expect(panel.get_by_role('alert')).to_contain_text('当前密码不正确')
        other = browser.new_context()
        other.request.post(BASE + '/auth/login', form={'password': PASSWORD})
        observer = other.new_page()
        observer.goto(BASE + '/healthz')
        observer.evaluate("""() => new Promise(resolve => {
          window.streamClosed = false;
          window.stream = new WebSocket(location.origin.replace('http','ws') + '/traffic');
          window.stream.onclose = () => { window.streamClosed = true };
          window.stream.onmessage = () => resolve(true);
        })""")
        panel.get_by_label('当前密码', exact=True).fill(PASSWORD)
        panel.get_by_role('button', name='保存新密码').click()
        page.wait_for_url('**/login*', timeout=30000)
        observer.wait_for_function('window.streamClosed === true')
        assert other.request.get(BASE + '/api/profiles').status == 401
        other.close()
        context.clear_cookies()
        rejected = context.request.post(BASE + '/auth/login', form={'password': PASSWORD})
        assert 'error=1' in rejected.url
        page.goto(BASE + '/login')
        page.get_by_label('访问密码').fill('integration-changed-password')
        page.get_by_role('button', name='登录', exact=True).click()
        page.wait_for_url('**/ui/zashboard/**')
        docker('restart', NAME)
        wait_ready()
        context.clear_cookies()
        rejected = context.request.post(BASE + '/auth/login', form={'password': PASSWORD})
        assert 'error=1' in rejected.url
        accepted = context.request.post(BASE + '/auth/login', form={'password': 'integration-changed-password'})
        assert accepted.status == 200 and '/ui/zashboard/' in accepted.url
        assert context.request.get(BASE + '/api/profiles').status == 200
        assert '已更新' in docker('exec', NAME, 'mymihomo', 'update')
        print('PASS password: old-password validation, session/WebSocket revocation, restart persistence, CLI after rotation')
        browser.close()
    print('PASS browser: login, native page, local import, global merge, WebSocket, mobile, logout')


def main():
    gateway = json.loads(docker('network', 'inspect', 'bridge'))[0]['IPAM']['Config'][0]['Gateway']
    fixture = http.server.ThreadingHTTPServer((gateway, 0), Fixture)
    threading.Thread(target=fixture.serve_forever, daemon=True).start()
    folder = Path(tempfile.mkdtemp(prefix='integration-', dir=ROOT / '.build'))
    conf = folder / 'conf'; conf.mkdir()
    (conf / 'config.yaml').write_text(YAML)
    source = f'http://host.docker.internal:{fixture.server_port}/valid'
    created = False
    try:
        docker('run', '-d', '--name', NAME, '--add-host=host.docker.internal:host-gateway',
               '-p', '127.0.0.1:19098:9097', '-p', '127.0.0.1:17897:7897',
               '-e', 'EXTERNAL_SECRET=integration-core-secret', '-e', f'WEB_CONFIG_SECRET={PASSWORD}',
               '-e', 'EXTERNAL_PORT=19097', '-e', 'WEB_CONFIG_PORT=9097', '-e', 'MIXED_PORT=7897',
               '-v', f'{conf}:/root/conf', 'local/dqgod-mymihomo:dev')
        created = True
        wait_ready()
        request('/api/profiles', expected=401)
        request('/configs', expected=401)
        login()
        assert b'zashboard' in request('/ui/zashboard/')
        request('/ui/yacd/', expected=404)
        request('/ui/dashboard/', expected=404)
        request('/upgrade/ui', 'POST', {}, 409)
        request('/api/profiles', 'POST', {'name': 'Demo 订阅', 'url': source, 'use_proxy': False, 'cron': '* * * * *'})
        data = request('/api/profiles'); remote = data['profiles'][-1]; rid = remote['id']
        assert remote['usage']['total'] == 53687091200
        request(f'/api/profiles/{rid}/activate', 'POST', {})
        request('/api/profiles/global', 'PUT', {'merge': 'prepend-rules:\n  - DOMAIN,merge.example,DIRECT\n', 'script': "function main(c) { c.rules.unshift('DOMAIN,script.example,REJECT'); return c; }", 'timezone': 'Asia/Hong_Kong'})
        rules = request('/rules')['rules']
        assert rules[0]['payload'] == 'script.example' and rules[1]['payload'] == 'merge.example', rules[:2]
        runtime = request('/api/profiles/runtime')['content']
        assert 'script.example' in runtime and '[已隐藏]' in runtime
        request('/configs', 'PATCH', {'mixed-port': 9999}, 409)
        request('/configs', 'PATCH', {'mode': 'global'}, 204)
        assert 'mode: global' in request('/api/profiles/runtime')['content']
        request('/configs', 'PATCH', {'mode': 'rule'}, 204)
        prior = (conf / 'config.yaml').read_bytes()
        request(f'/api/profiles/{rid}/extensions', 'PUT', {'merge': '', 'script': 'function main(c) { while(true) {} }'}, 400)
        assert prior == (conf / 'config.yaml').read_bytes()
        request(f'/api/profiles/{rid}/content', 'PUT', {'content': 'rules: [', 'section': ''}, 403)
        assert prior == (conf / 'config.yaml').read_bytes()
        request(f'/api/profiles/{rid}', 'PUT', {'url': source.replace('/valid', '/invalid')})
        request(f'/api/profiles/{rid}/update', 'POST', {}, 400)
        assert prior == (conf / 'config.yaml').read_bytes()
        assert request('/api/profiles')['profiles'][-1]['last_error']
        request(f'/api/profiles/{rid}', 'PUT', {'url': source})
        request(f'/api/profiles/{rid}/update', 'POST', {})
        request(f'/api/profiles/{rid}', 'DELETE', expected=400)
        # Force a due schedule in the private test fixture, then allow the real timer to run.
        store = json.loads((conf / 'profiles.json').read_text())
        for p in store['profiles']:
            if p['id'] == rid: p['attempted_at'] = '2026-01-01T00:00:00Z'
        before = len(requests_seen)
        (conf / 'profiles.json').write_text(json.dumps(store))
        for _ in range(22):
            if len(requests_seen) > before: break
            time.sleep(1)
        assert len(requests_seen) > before, 'scheduled update not executed'
        assert request('/api/profiles')['profiles'][-1]['last_error'] == ''
        docker('restart', NAME)
        wait_ready()
        request('/api/profiles', expected=401)
        login()
        restored = request('/api/profiles')
        assert restored['active_id'] == rid and restored['global_script'] and restored['timezone'] == 'Asia/Hong_Kong'
        configs = request('/configs')
        assert configs['mixed-port'] == 7897
        print('PASS real core: CRUD, metadata, activate, extension order, failed update retention, cron, restart persistence')
        browser_checks()
    except Exception:
        print(docker('logs', '--tail', '35', NAME) if created else 'test container not created')
        raise
    finally:
        if created: docker('rm', '-f', NAME)
        fixture.shutdown()


if __name__ == '__main__':
    main()
