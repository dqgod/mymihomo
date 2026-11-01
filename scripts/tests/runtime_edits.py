#!/usr/bin/env python3
"""Runtime editing checks against an isolated core and the real web UI."""
import json
import tempfile
import threading
import http.server
from pathlib import Path
import integration as h
from playwright.sync_api import sync_playwright, expect


def snapshot(): return h.request('/api/runtime-edits')
def row(kind, name):
    return next(item for item in snapshot()[kind] if (name in item['value'] if kind == 'rules' else item['value']['name'] == name))
def mutate(action, expected=200):
    state=snapshot()
    return h.request('/api/runtime-edits','POST',dict(profile_id=state['profile_id'],revision=state['revision'],**action),expected)


def browser_checks():
    with sync_playwright() as pw:
        browser=pw.chromium.launch(headless=True,args=['--no-sandbox','--disable-dev-shm-usage'])
        page=browser.new_page(viewport={'width':1440,'height':1000},locale='zh-CN')
        errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
        page.goto(h.BASE+'/login');page.get_by_label('访问密码').fill(h.PASSWORD);page.get_by_role('button',name='登录',exact=True).click();page.wait_for_url('**/ui/zashboard/**')
        page.goto(h.BASE+'/ui/zashboard/#/proxies')
        page.get_by_role('button',name='编辑代理组',exact=True).click()
        page.get_by_role('heading',name='代理组编辑',exact=True).wait_for()
        page.get_by_role('button',name='新增代理组',exact=True).click()
        dialog=page.get_by_role('dialog')
        dialog.get_by_label('组名',exact=True).fill('Browser Group')
        dialog.get_by_label('选择方式',exact=True).select_option('url-test')
        dialog.get_by_role('button',name='保存并应用').click();dialog.wait_for(state='hidden',timeout=30000)
        page.get_by_role('combobox',name='变更筛选').select_option('added')
        card=page.locator('article').filter(has_text='Browser Group')
        expect(card).to_be_visible()
        card.get_by_role('button',name='编辑',exact=True).click()
        dialog.get_by_label('选择方式',exact=True).select_option('select')
        dialog.get_by_role('button',name='保存并应用').click();dialog.wait_for(state='hidden',timeout=30000)
        expect(card).to_contain_text('select')
        card.get_by_role('checkbox').check()
        page.get_by_role('button',name='删除选中新增项',exact=False).click();page.get_by_role('button',name='确认',exact=True).click();page.get_by_role('dialog').wait_for(state='hidden',timeout=30000)
        expect(page.locator('article').filter(has_text='Browser Group')).to_have_count(0)
        page.get_by_role('combobox',name='变更筛选').select_option('all')
        original=page.locator('article').filter(has=page.get_by_text('Test',exact=True))
        original.get_by_role('button',name='编辑',exact=True).click()
        dialog.get_by_label('选择方式',exact=True).select_option('url-test')
        dialog.get_by_role('button',name='保存并应用').click();dialog.wait_for(state='hidden',timeout=30000)
        page.get_by_role('combobox',name='变更筛选').select_option('modified')
        original.get_by_role('button',name='对比原配置',exact=True).click()
        dialog.get_by_role('textbox',name='当前配置',exact=True).focus();page.keyboard.press('Control+f')
        expect(dialog.get_by_role('searchbox',name='搜索当前配置')).to_have_count(1)
        dialog.get_by_role('searchbox',name='搜索当前配置').fill('interval')
        expect(dialog.get_by_role('status')).to_contain_text('1 / 1')
        page.keyboard.press('Escape');dialog.get_by_role('button',name='关闭',exact=True).click()
        original.get_by_role('button',name='恢复默认',exact=True).click();dialog.get_by_role('button',name='确认',exact=True).click();dialog.wait_for(state='hidden',timeout=30000)
        expect(page.locator('article')).to_have_count(0)
        page.goto(h.BASE+'/ui/zashboard/#/rules')
        page.get_by_role('button',name='编辑规则',exact=True).click()
        page.get_by_role('button',name='新增规则',exact=True).click()
        dialog.get_by_label('匹配内容',exact=True).fill('browser.example')
        dialog.get_by_role('button',name='保存并应用').click();dialog.wait_for(state='hidden',timeout=30000)
        page.get_by_role('combobox',name='变更筛选').select_option('added')
        card=page.locator('article').filter(has_text='browser.example');expect(card).to_be_visible()
        card.get_by_role('checkbox',name='启用 DOMAIN-SUFFIX,browser.example,DIRECT',exact=True).uncheck()
        expect(card).to_contain_text('禁用')
        page.get_by_role('button',name='规则集 0',exact=True).click()
        page.get_by_role('button',name='新增规则集',exact=True).click()
        dialog.get_by_label('规则集名称',exact=True).fill('Browser Provider')
        dialog.get_by_role('button',name='保存并应用').click();dialog.wait_for(state='hidden',timeout=30000)
        expect(page.locator('article').filter(has_text='Browser Provider')).to_be_visible()
        shots=h.ROOT/'.build/screenshots';shots.mkdir(exist_ok=True,parents=True)
        page.screenshot(path=str(shots/'runtime-providers.png'),full_page=True)
        page.set_viewport_size({'width':390,'height':844})
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
        page.screenshot(path=str(shots/'runtime-mobile.png'),full_page=True)
        assert not errors,errors
        browser.close()
    print('PASS browser: group select/url-test, rule create/disable, provider create, filters, batch deletion, mobile')


def main():
    gateway=json.loads(h.docker('network','inspect','bridge'))[0]['IPAM']['Config'][0]['Gateway']
    fixture=http.server.ThreadingHTTPServer((gateway,0),h.Fixture)
    threading.Thread(target=fixture.serve_forever,daemon=True).start()
    folder=Path(tempfile.mkdtemp(prefix='runtime-integration-',dir=h.ROOT/'.build'));conf=folder/'conf';conf.mkdir()
    (conf/'config.yaml').write_text(h.YAML)
    created=False
    try:
        h.docker('run','-d','--name',h.NAME,'--add-host=host.docker.internal:host-gateway','-p','127.0.0.1:19098:9097','-p','127.0.0.1:17897:7897','-e','EXTERNAL_SECRET=integration-core-secret','-e','WEB_CONFIG_SECRET='+h.PASSWORD,'-v',str(conf)+':/root/conf','local/dqgod-mymihomo:dev')
        created=True;h.wait_ready();h.request('/api/runtime-edits',expected=401);h.login()
        h.request('/api/profiles','POST',{'name':'Remote','url':f'http://host.docker.internal:{fixture.server_port}/valid','use_proxy':False})
        rid=h.request('/api/profiles')['profiles'][-1]['id'];h.request(f'/api/profiles/{rid}/activate','POST',{})
        original=h.request(f'/api/profiles/{rid}')
        group=row('groups','Test')
        h.request('/configs','PATCH',{'mode':'global'},204)
        mutate({'action':'edit','id':group['id'],'yaml':'name: Renamed\ntype: url-test\nproxies: [DIRECT, REJECT]\nurl: https://example.test/204\ninterval: 3600'})
        assert h.request('/configs')['mode']=='global'
        h.request('/configs','PATCH',{'mode':'rule'},204)
        assert h.request('/proxies/Renamed')['type']=='URLTest'
        assert h.request('/rules')['rules'][-1]['proxy']=='Renamed'
        mutate({'action':'create','kind':'providers','yaml':"name: Custom\ntype: inline\nbehavior: domain\npayload: ['+.custom.example']"})
        provider=row('providers','Custom')
        mutate({'action':'create','kind':'rules','yaml':'RULE-SET,Custom,Renamed'})
        mutate({'action':'toggle','id':provider['id'],'disabled':True})
        assert not any(r['type']=='RuleSet' for r in h.request('/rules')['rules'])
        assert row('rules','RULE-SET')['effective_enabled'] is False
        mutate({'action':'toggle','id':provider['id'],'disabled':False})
        assert any(r['type']=='RuleSet' for r in h.request('/rules')['rules'])
        before=(conf/'config.yaml').read_bytes()
        mutate({'action':'delete','id':provider['id']},400)
        assert before==(conf/'config.yaml').read_bytes()
        mutate({'action':'create','kind':'groups','yaml':'name: Loop\ntype: select\nproxies: [Loop]'},400)
        assert before==(conf/'config.yaml').read_bytes()
        source_yaml = h.YAML
        h.YAML = h.YAML.replace('[DIRECT, REJECT]', '[DIRECT, REJECT, PASS]')
        h.request(f'/api/profiles/{rid}/update','POST',{})
        assert len(row('groups','Renamed')['value']['proxies']) == 3
        h.YAML = h.YAML.replace('  - name: Test\n    type: select\n    proxies: [DIRECT, REJECT, PASS]\n', '')
        h.request(f'/api/profiles/{rid}/update','POST',{})
        missing = row('groups','Renamed')
        assert missing['missing'] and len(missing['value']['proxies']) == 3
        mutate({'action':'restore','id':group['id']},400)
        h.YAML = source_yaml
        h.request(f'/api/profiles/{rid}/update','POST',{})
        assert row('groups','Renamed')['missing'] is False
        assert row('groups','Renamed')['status']=='modified'
        assert h.request(f'/api/profiles/{rid}')['content']==original['content']
        h.docker('restart',h.NAME);h.wait_ready();h.login()
        assert row('groups','Renamed')['status']=='modified'
        assert row('providers','Custom')['status']=='added'
        assert '已更新' in h.docker('exec',h.NAME,'mymihomo','update')
        mutate({'action':'restore','id':group['id']})
        assert h.request('/proxies/Test')['type']=='Selector'
        assert row('rules','RULE-SET')['value']=='RULE-SET,Custom,Test'
        rule=row('rules','RULE-SET')
        mutate({'action':'delete','ids':[rule['id'],provider['id']]})
        assert not snapshot()['providers']
        stale=snapshot()
        mutate({'action':'create','kind':'rules','yaml':'DOMAIN,stale.example,DIRECT'})
        h.request('/api/runtime-edits','POST',{'profile_id':rid,'revision':stale['revision'],'action':'restore','id':group['id']},409)
        mutate({'action':'delete','id':row('rules','stale.example')['id']})
        h.request('/rules/disable','PATCH',{'0':True},409)
        print('PASS core: runtime overlays, references, provider disable, rename/restore, update/restart/CLI persistence, conflicts and rollback')
        browser_checks()
    except Exception:
        if created:print(h.docker('logs','--tail','25',h.NAME))
        raise
    finally:
        if created:h.docker('rm','-f',h.NAME)
        fixture.shutdown()
if __name__=='__main__':main()
