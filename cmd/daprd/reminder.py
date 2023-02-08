import json

import requests

# 	Data      interface{} `json:"data"`
# 	DueTime   string      `json:"dueTime"`
# 	Period    string      `json:"period"`
# 	TTL       string      `json:"ttl"`

#
# actors/{actorType}/{actorId}/reminders/{name}
# requests.delete('http://localhost:3500/v1.0/actors/actorType-a/actorId-a/reminders/2daa32f1-238a-482c-af0b-9d05c42a643e')

data = {
    'Period': "5s",  # 0h30m0s
    'TTL': "30m",  # R5/PT30M  0h30m0s
    'DueTime': "0h",  # R5/PT30M  0h30m0s
    'Data': {},
}
res = requests.post('http://localhost:3500/v1.0/actors/actorType-a/actorId-a/reminders/demo', json.dumps(data))
print(res.text)
