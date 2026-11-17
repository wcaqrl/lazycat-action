import json
import threading
import unittest
import urllib.request
from unittest.mock import patch
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer

from github_api import github_json

class ReaderTests(unittest.TestCase):
    def server(self,handler):
        server=ThreadingHTTPServer(('127.0.0.1',0),handler)
        thread=threading.Thread(target=server.serve_forever,kwargs={'poll_interval':0.01},daemon=True);thread.start()
        self.addCleanup(lambda:(server.shutdown(),server.server_close(),thread.join()))
        return 'http://127.0.0.1:'+str(server.server_port)

    def test_managed_reader_receives_only_its_local_session(self):
        seen=[]
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                seen.append((self.path,self.headers.get('Authorization'),self.headers.get('X-Lazycat-GitHub-Session')))
                self.send_response(200);self.end_headers();self.wfile.write(b'{"tag_name":"v1.2.3"}')
            def log_message(self,*args):pass
        base=self.server(Handler)
        result=github_json('https://api.github.com/repos/acme/app/releases/latest',{'LAZYCAT_GITHUB_API_URL':base,'LAZYCAT_GITHUB_API_SESSION':'a'*64,'GITHUB_TOKEN':'must-not-reach-reader','GITHUB_ACTIONS':'true'})
        self.assertEqual(result,{'tag_name':'v1.2.3'})
        self.assertEqual(seen,[('/repos/acme/app/releases/latest',None,'a'*64)])

    def test_ci_without_managed_access_does_not_silently_use_anonymous_api(self):
        with self.assertRaisesRegex(RuntimeError,'authenticated'):
            github_json('https://api.github.com/repos/acme/app/releases/latest',{'GITHUB_ACTIONS':'true'})

    def test_foreign_urls_and_invalid_local_endpoints_are_rejected(self):
        for url in ('http://api.github.com/repos/acme/app/releases/latest','https://api.github.com.evil/repos/acme/app/releases/latest','https://api.github.com@evil.example/repos/acme/app/releases/latest','https://api.github.com/user'):
            with self.subTest(url=url),self.assertRaises(ValueError):github_json(url,{})
        with self.assertRaises(ValueError):github_json('https://api.github.com/repos/acme/app/releases/latest',{'LAZYCAT_GITHUB_API_URL':'https://evil.example','LAZYCAT_GITHUB_API_SESSION':'a'*64})

    def test_redirects_and_managed_failures_are_not_retried_or_followed(self):
        foreign=[]
        class Foreign(BaseHTTPRequestHandler):
            def do_GET(self):foreign.append(self.path);self.send_response(200);self.end_headers()
            def log_message(self,*args):pass
        destination=self.server(Foreign);calls=[]
        class Redirect(BaseHTTPRequestHandler):
            def do_GET(self):calls.append(self.path);self.send_response(302);self.send_header('Location',destination);self.end_headers()
            def log_message(self,*args):pass
        base=self.server(Redirect)
        with self.assertRaisesRegex(RuntimeError,'302'):github_json('https://api.github.com/repos/acme/app/releases/latest',{'LAZYCAT_GITHUB_API_URL':base,'LAZYCAT_GITHUB_API_SESSION':'a'*64})
        self.assertEqual(foreign,[]);self.assertEqual(len(calls),1)

    def test_secondary_rate_limit_without_wait_header_fails_without_early_retry(self):
        calls=[]
        class Limited(BaseHTTPRequestHandler):
            def do_GET(self):
                calls.append(self.path);self.send_response(403);self.end_headers()
                self.wfile.write(b'{"message":"secondary rate limit exceeded"}')
            def log_message(self,*args):pass
        base=self.server(Limited)
        local=urllib.request.build_opener(urllib.request.ProxyHandler({}))
        class Transport:
            def open(self,request,timeout):
                return local.open(urllib.request.Request(base+'/repos/acme/app/releases/latest',headers=request.headers),timeout=timeout)
        with patch('github_api.urllib.request.build_opener',return_value=Transport()),patch('github_api.time.sleep') as sleep:
            with self.assertRaisesRegex(RuntimeError,'rate limit'):
                github_json('https://api.github.com/repos/acme/app/releases/latest',{'GITHUB_TOKEN':'token'})
            self.assertEqual(len(calls),1);sleep.assert_not_called()

if __name__=='__main__':unittest.main()
