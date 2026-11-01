#!/usr/bin/env python3
"""Test the rule-policy picker and capture documentation screenshots with fake data only."""
import json
import re
import tempfile
from pathlib import Path
from urllib.parse import quote
import yaml
from playwright.sync_api import sync_playwright, expect
import integration as h

NODES = ['香港 · 演示 A', '香港 · 演示 B', '日本 · 演示 A', '日本 · 演示 B', '新加坡 · 演示', '美国 · 演示']
MAIN = '🚀 节点选择'
AUTO = '♻ 自动选择'
GLOBAL = '🌐 国际服务'
MEDIA = '🎬 流媒体'
DEMO = {
    'proxies': [{'name': name, 'type': 'http', 'server': '127.0.0.1', 'port': 9} for name in NODES],
    'proxy-groups': [
        {'name': MAIN, 'type': 'select', 'proxies': [AUTO, *NODES, 'DIRECT']},
        {'name': AUTO, 'type': 'url-test', 'url': 'http://127.0.0.1:9/generate_204', 'interval': 86400, 'lazy': True, 'proxies': NODES},
        {'name': GLOBAL, 'type': 'select', 'proxies': [MAIN, AUTO, *NODES[:3], 'DIRECT']},
        {'name': MEDIA, 'type': 'select', 'proxies': [MAIN, *NODES[2:]]},
    ],
    'rules': [f'DOMAIN-SUFFIX,example.com,{MAIN}', f'DOMAIN-SUFFIX,video.example,{MEDIA}', 'DOMAIN-SUFFIX,ads.example,REJECT', 'GEOIP,private,DIRECT,no-resolve', 'MATCH,DIRECT'],
}


def change(body):
    state = h.request('/api/runtime-edits')
    return h.request('/api/runtime-edits', 'POST', dict(profile_id=state['profile_id'], revision=state['revision'], **body))


def prepare_demo():
    first = h.request('/api/profiles')['profiles'][0]
    h.request('/api/profiles/' + first['id'], 'PUT', {'name': '日常订阅 · 演示', 'url': 'https://subscription.example/daily', 'cron': '30 12 * * *'})
    h.request('/api/profiles', 'POST', {'name': '备用配置 · 演示', 'content': yaml.safe_dump(DEMO, allow_unicode=True), 'use_proxy': False})
    h.request('/api/profiles/global', 'PUT', {'merge': 'prepend-rules:\n  - DOMAIN-SUFFIX,local.example,DIRECT\n', 'script': 'function main(config) { return config; }', 'timezone': 'Asia/Hong_Kong'})
    change({'action': 'create', 'kind': 'groups', 'yaml': yaml.safe_dump({'name': '🧪 自定义策略', 'type': 'select', 'proxies': [MAIN, 'DIRECT']}, allow_unicode=True)})
    change({'action': 'create', 'kind': 'rules', 'yaml': 'DOMAIN-SUFFIX,docs.example,DIRECT'})
    state = h.request('/api/runtime-edits')
    target = next(r for r in state['rules'] if 'example.com' in r['value'])
    change({'action': 'edit', 'id': target['id'], 'yaml': f'DOMAIN-SUFFIX,example.com,{GLOBAL}'})
    target = next(r for r in state['rules'] if 'ads.example' in r['value'])
    change({'action': 'toggle', 'id': target['id'], 'disabled': True})


def picker_checks(page, mobile=False):
    page.goto(h.BASE + '/ui/zashboard/#/rules')
    page.get_by_role('button', name='编辑规则', exact=True).click()
    page.get_by_role('button', name='新增规则', exact=True).click()
    dialog = page.get_by_role('dialog')
    combo = dialog.get_by_role('combobox', name='目标策略 / 代理组', exact=True)
    expect(combo).to_contain_text('DIRECT')
    if mobile: combo.tap()
    else: combo.click()
    all_options = set(h.request('/api/runtime-edits')['proxy_options'])
    expect(page.get_by_role('listbox').get_by_role('option')).to_have_count(len(all_options))
    assert set(value.strip() for value in page.get_by_role('listbox').get_by_role('option').all_text_contents()) == all_options
    expect(page.get_by_role('listbox')).to_be_in_viewport()
    if mobile:
        page.set_viewport_size({'width':390,'height':520})
        page.wait_for_timeout(300)
        popup=page.get_by_role('listbox').locator('..').bounding_box()
        assert popup and popup['y'] >= 0 and popup['y']+popup['height'] <= 520
        page.set_viewport_size({'width':390,'height':844})
    search = page.get_by_role('searchbox', name='搜索策略', exact=True)
    search.fill('流媒体');expect(page.get_by_role('listbox').get_by_role('option')).to_have_count(1)
    page.get_by_role('listbox').get_by_role('option', name=MEDIA, exact=True).click()
    expect(combo).to_contain_text(MEDIA)
    combo.click();expect(search).to_have_value('');expect(page.get_by_role('listbox').get_by_role('option')).to_have_count(len(all_options))
    search.fill('不存在的策略');expect(page.get_by_role('listbox').get_by_role('option')).to_have_count(0)
    expect(combo).to_contain_text(MEDIA)
    search.press('Escape');expect(page.get_by_role('listbox')).to_have_count(0);expect(dialog).to_be_visible()
    combo.press('ArrowDown');search.fill('国际服务');search.press('Enter');expect(combo).to_contain_text(GLOBAL)
    expect(dialog).to_be_visible()  # Enter selects rather than submitting the form.
    combo.click();dialog.get_by_role('heading').click();expect(page.get_by_role('listbox')).to_have_count(0)
    dialog.get_by_label('匹配内容', exact=True).fill('picker-test.example' if not mobile else 'mobile-picker.example')
    dialog.get_by_role('button', name='保存并应用').click();dialog.wait_for(state='hidden', timeout=30000)
    # Re-open an existing rule and ensure its current value does not filter the list.
    row = page.locator('article').filter(has_text='picker-test.example' if not mobile else 'mobile-picker.example')
    row.get_by_role('button', name='编辑', exact=True).click()
    combo.click();expect(page.get_by_role('listbox').get_by_role('option')).to_have_count(len(all_options))
    option = page.get_by_role('listbox').get_by_role('option', name='DIRECT', exact=True)
    if mobile: option.tap()
    else: option.click()
    expect(combo).to_contain_text('DIRECT')
    combo.press('Escape')
    dialog.wait_for(state='hidden')
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    state=h.request('/api/runtime-edits')
    created=next(r for r in state['rules'] if ('mobile-picker.example' if mobile else 'picker-test.example') in r['value'])
    assert created['value'].endswith(',' + GLOBAL), 'Selected policy was not saved or cancel changed it'
    change({'action':'delete','id':created['id']})


def capture(page):
    folder = h.ROOT/'docs/images';folder.mkdir(parents=True, exist_ok=True)
    def demo_metadata(route):
        response=route.fetch();payload=response.json()
        if response.status==200 and 'profiles' in payload:
            for i,profile in enumerate(payload['profiles']):
                if profile['url']:
                    profile['usage']={'upload':256*1024**2,'download':int(12.5*1024**3),'total':100*1024**3,'expire':1893456000}
            route.fulfill(json=payload)
        else:route.fulfill(response=response)
    def demo_delays(route):
        response=route.fetch();payload=response.json()
        if response.status==200 and 'proxies' in payload:
            for i,name in enumerate(NODES):
                if name in payload['proxies']:
                    payload['proxies'][name]['history']=[{'time':'2026-01-01T00:00:00Z','delay':[42,58,79,91,128,186][i]}]
            route.fulfill(json=payload)
        else:route.fulfill(response=response)
    page.route(re.compile(re.escape(h.BASE)+r'/api/profiles$'),demo_metadata)
    page.route(re.compile(re.escape(h.BASE)+r'/proxies$'),demo_delays)
    page.goto(h.BASE+'/ui/zashboard/#/subscriptions');page.reload()
    page.locator('article').first.wait_for();page.wait_for_timeout(1000)
    page.screenshot(path=str(folder/'subscriptions.png'))
    page.goto(h.BASE+'/ui/zashboard/#/proxies');page.reload()
    page.get_by_role('button',name='定位当前节点',exact=True).first.wait_for()
    page.wait_for_timeout(800);page.screenshot(path=str(folder/'proxies.png'))
    page.goto(h.BASE+'/ui/zashboard/#/rules');page.get_by_role('button',name='编辑规则',exact=True).click()
    page.locator('article').first.wait_for();page.wait_for_timeout(500)
    page.screenshot(path=str(folder/'runtime-rules.png'))
    page.get_by_role('button',name='新增规则',exact=True).click()
    dialog=page.get_by_role('dialog');dialog.get_by_label('匹配内容',exact=True).fill('docs.example')
    combo=dialog.get_by_role('combobox',name='目标策略 / 代理组',exact=True);combo.click()
    expect(page.get_by_role('listbox')).to_be_visible();page.wait_for_timeout(300)
    page.screenshot(path=str(folder/'policy-picker.png'))
    page.get_by_role('searchbox',name='搜索策略',exact=True).press('Escape')
    dialog.get_by_role('button',name='取消',exact=True).click()
    page.unroute_all(behavior='wait')


def browser_checks():
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True,args=['--no-sandbox','--disable-dev-shm-usage'])
        context=browser.new_context(viewport={'width':1360,'height':980},locale='zh-CN',color_scheme='light')
        # Documentation uses expanded navigation and deterministic demonstration data.
        context.add_init_script("localStorage.setItem('config/is-sidebar-collapsed','false'); localStorage.setItem('cache/collapse-group-map',JSON.stringify({" + ','.join(json.dumps(name)+':true' for name in [MAIN,AUTO,GLOBAL,MEDIA]) + '}));')
        page=context.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
        page.goto(h.BASE+'/login');page.get_by_label('访问密码').fill(h.PASSWORD);page.get_by_role('button',name='登录',exact=True).click();page.wait_for_url('**/ui/zashboard/**')
        picker_checks(page)
        capture(page)
        mobile=browser.new_context(viewport={'width':390,'height':844},locale='zh-CN',is_mobile=True,has_touch=True,color_scheme='dark')
        phone=mobile.new_page();phone.on('pageerror',lambda e:errors.append(str(e)))
        phone.goto(h.BASE+'/login');phone.get_by_label('访问密码').fill(h.PASSWORD);phone.get_by_role('button',name='登录',exact=True).tap();phone.wait_for_url('**/ui/zashboard/**')
        picker_checks(phone,True)
        assert not errors,errors
        browser.close()
    print('PASS desktop/mobile: full list, existing-value reopen, search, keyboard, Escape/outside click, save, screenshots with demo data')


def main():
    folder=Path(tempfile.mkdtemp(prefix='policy-demo-',dir=h.ROOT/'.build'));conf=folder/'conf';conf.mkdir()
    (conf/'config.yaml').write_text(yaml.safe_dump(DEMO, allow_unicode=True,sort_keys=False))
    created=False
    try:
        h.docker('run','-d','--name',h.NAME,'-p','127.0.0.1:19098:9097','-e','EXTERNAL_SECRET=integration-core-secret','-e','WEB_CONFIG_SECRET='+h.PASSWORD,'-v',str(conf)+':/root/conf','local/dqgod-mymihomo:dev')
        created=True;h.wait_ready();h.login();prepare_demo();browser_checks()
    except Exception:
        if created:print(h.docker('logs','--tail','15',h.NAME))
        raise
    finally:
        if created:h.docker('rm','-f',h.NAME)
if __name__=='__main__':main()
