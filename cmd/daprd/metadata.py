import json
import pprint

import requests

# INFO[0000] GET----/v1.0/metadata
# INFO[0000] PUT----/v1.0/metadata/{key}

data = {
    'Period': "5s",  # 0h30m0s
    'TTL': "30m",  # R5/PT30M  0h30m0s
    'DueTime': "0h",  # R5/PT30M  0h30m0s
    'Data': {},
}
# res = requests.post('http://localhost:3500/v1.0/actors/a||b/c||d/reminders/demo', json.dumps(data))
# requests.delete('http://localhost:3500/v1.0/actors/actorType-a/actorId-a/reminders/demo', )
# res = requests.post('http://localhost:3500/v1.0/actors/actorType-a/actorId-a/reminders/demo', json.dumps(data))
# print(res.text)

dapr_url = "http://localhost:3500/v1.0/metadata/"
pprint.pprint(requests.get(dapr_url).json(), indent=4, )
# {   'actors': [   {'count': 0, 'type': 'a||b'},
#                   {'count': 0, 'type': 'actorType-a'},
#                   {'count': 0, 'type': 'actorType-b'},
#                   {'count': 0, 'type': 'actorType-c'}],
#     'components': [   {   'name': 'pubsub',
#                           'type': 'pubsub.redis',
#                           'version': 'v1'},
#                       {   'name': 'statestore',
#                           'type': 'state.redis',
#                           'version': 'v1'}],
#     'extended': {},
#     'id': 'app01'}

# res = requests.post('http://localhost:3500/v1.0/actors/actorType-a/actorId-a/timer/demo', json.dumps(data))
# print(res.text)
