"""LazyCat GitHub metadata client, vendorable without third-party dependencies.

Pipeline scripts use the executor's scoped, short-lived reader. The real GitHub
token stays in the executor. Standalone trusted workflow steps may explicitly
provide GITHUB_TOKEN; local interactive checks may use anonymous public reads.
"""
import json
from email.utils import parsedate_to_datetime
import os
import re
import time
import urllib.error
import urllib.parse
import urllib.request

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args,**kwargs):return None

def github_json(url,environ=None):
    env=os.environ if environ is None else environ
    parsed=urllib.parse.urlsplit(url)
    if parsed.scheme!='https' or parsed.hostname!='api.github.com' or parsed.port not in (None,443) or parsed.username or parsed.fragment or not parsed.path.startswith('/repos/'):
        raise ValueError('Expected a GitHub repository metadata URL')
    path=parsed.path+('?' + parsed.query if parsed.query else '')
    headers={'Accept':'application/vnd.github+json','User-Agent':'lazycat-action-python','X-GitHub-Api-Version':'2022-11-28'}
    base=env.get('LAZYCAT_GITHUB_API_URL','');session=env.get('LAZYCAT_GITHUB_API_SESSION','')
    managed=bool(base or session)
    if managed:
        local=urllib.parse.urlsplit(base)
        if local.scheme!='http' or local.hostname!='127.0.0.1' or not local.port or local.username or local.path not in ('','/') or local.query or local.fragment or not re.fullmatch('[a-f0-9]{64}',session):
            raise ValueError('Invalid managed GitHub reader endpoint/session')
        target=base.rstrip('/')+path;headers['X-Lazycat-GitHub-Session']=session
        opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
    else:
        token=env.get('GITHUB_TOKEN') or env.get('GH_TOKEN')
        if token:headers['Authorization']='Bearer '+token
        elif env.get('GITHUB_ACTIONS')=='true':raise RuntimeError('GitHub Actions requires authenticated metadata access; use lazycat-action v1.10.0+ or an explicit workflow GITHUB_TOKEN')
        target=url;opener=urllib.request.build_opener(NoRedirect())
    budget=30.0;attempts=1 if managed else 3
    for attempt in range(attempts):
        try:
            request=urllib.request.Request(target,headers=headers)
            with opener.open(request,timeout=120 if managed else 20) as response:raw=response.read(4*1024*1024+1)
            if len(raw)>4*1024*1024:raise ValueError('Oversized GitHub metadata')
            return json.loads(raw)
        except urllib.error.HTTPError as error:
            raw=error.read(4096).decode(errors='replace')
            limited=error.code==429 or (error.code==403 and (error.headers.get('X-RateLimit-Remaining')=='0' or error.headers.get('Retry-After') is not None or 'rate limit' in raw.lower()))
            retry=limited or error.code in (500,502,503,504)
            delay=60.0 if limited else float(2**attempt)
            if error.headers.get('Retry-After','').isdigit():delay=float(error.headers['Retry-After'])
            elif error.headers.get('Retry-After'):
                try:delay=max(0,parsedate_to_datetime(error.headers['Retry-After']).timestamp()-time.time())
                except (ValueError,TypeError,OverflowError):pass
            elif error.headers.get('X-RateLimit-Remaining')=='0' and error.headers.get('X-RateLimit-Reset','').isdigit():delay=max(0,float(error.headers['X-RateLimit-Reset'])-time.time())
            if not managed and retry and attempt+1<attempts and delay<=budget:
                time.sleep(delay);budget-=delay;continue
            kind='rate limit reached' if limited else 'request failed'
            # Never include token values, local capability URLs or raw upstream
            # bodies in command/build diagnostics.
            raise RuntimeError(f'GitHub metadata {kind}: HTTP {error.code} at {path}') from None
        except (OSError,urllib.error.URLError):
            delay=float(2**attempt)
            if not managed and attempt+1<attempts and delay<=budget:
                time.sleep(delay);budget-=delay;continue
            raise RuntimeError('GitHub metadata connection failed at '+path) from None
