import json

import requests

#  rawPayload 决定是不是真的发送到组件，如果是true; 则------ todo
metadata = {
    # 'ttlInSeconds': '3',
    # 'rawPayload': 'true'
}
query = '&'.join(["metadata.%s=%s" % (k, v) for k, v in metadata.items()])
# PUB
for i in range(2):
    print(requests.post('http://localhost:3500/v1.0/publish/redis-pubsub/topic-b?' + query, json.dumps(
        {
            "demo": "test"
        }
    )).text)

# 在Redis内部使用是
# XADD
#
