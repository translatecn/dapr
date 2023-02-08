import json
import random

import requests

dapr_url = "http://localhost:3500/v1.0/bindings/myevent"
res = requests.post(dapr_url, json.dumps({
    'data': {'message': random.random() * 1000},
    'metadata': {
        'key': 'a'
    },
    'operation': 'create'}))
print(res.json())
