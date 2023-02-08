import requests

#  实时响应的, 只要程序的系统变量发生变化，这里就可以看到
print(requests.get('http://localhost:3500/v1.0/secrets/env-secret/bulk').text)
print(requests.get('http://localhost:3500/v1.0/secrets/env-secret/PWD').text)
