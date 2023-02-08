import json

import requests

# print(requests.get('http://localhost:3500/v1.0/state/redis-statestore/b'))
# print(requests.post('http://localhost:3500/v1.0/state/redis-statestore', json.dumps(
#     [
#         {
#             "key": '1a',
#             'value': 1232,
#             'etag': "2",
#             # "metadata": {
#             #     "ttlInSeconds": "5"
#             # }
#         },
#     ]
# )).text)
# print(requests.get('http://localhost:3500/v1.0/state/redis-statestore/1a').text)
# If-Match 如果设置了必须与存储的版本一致
# print(requests.delete('http://localhost:3500/v1.0/state/redis-statestore/1a', headers={"If-Match": "444"}))

store_name = "redis-store" # name of the state store as specified in state store component yaml file
dapr_state_url = "http://localhost:3500/v1.0/state/{}".format(store_name)
response = requests.get(dapr_state_url + "/key1", headers={"concurrency":"first-write"})
etag = response.headers['ETag']
newState = '[{ "key": "k1", "value": "New Data", "etag": {}, "options": { "concurrency": "first-write" }}]'.format(etag)

requests.post(dapr_state_url, json=newState)
response = requests.delete(dapr_state_url + "/key1", headers={"If-Match": "{}".format(etag)})

# print(requests.post('http://localhost:3500/v1.0/state/redis-statestore/bulk', json.dumps({
#     'keys': ["1a", "b"],
#     'parallelism': 4,  # 并发度
#     # 'metadata': {
#     #     'a': 1
#     # }
# })).text)

# state/{storeName}/transaction
# print(requests.post('http://localhost:3500/v1.0/state/redis-statestore/transaction', json.dumps({
#     'operations': [
#         {
#             'operation': 'upsert',
#             'request': {
#                 'key': "a",
#                 'value': {'use': 'test'},
#                 'etag': "2",
#                 "metadata": {
#                     "ttlInSeconds": "500"
#                 }
#             }
#         },
#         {
#             'operation': 'delete',
#             'request': {
#                 'key': "b",
#                 'etag': "2",
#             }
#         }
#     ],
#     # 'metadata': {
#     #     'a': 1
#     # }
# })).text)
# print(requests.get('http://localhost:3500/v1.0/state/redis-statestore/a').text)
print(requests.post('http://localhost:3500/v1.0-alpha1/state/redis-statestore/query', json.dumps(
    {
        'query': {
            'filters': {
            },
            'sort': [{
                'key': "",
                "order": ""
            }],
            'page': {
                "limit": 1,
                'token': ""
            }
        }
    }
)))
