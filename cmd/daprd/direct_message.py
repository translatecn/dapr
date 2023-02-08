url = 'http://127.0.0.1:3001/post'
dapr_url = "http://localhost:3500/v1.0/invoke/dp-61c2cb20562850d49d47d1c7-executorapp/method/health"

# dapr_url = "http://localhost:3500/v1.0/healthz"

# res = requests.post(dapr_url, json.dumps({'a': random.random() * 1000}))
# res = requests.get(dapr_url, )
#
#
#
# print(res.text)
# print(res.status_code)

# INFO[0000] *----/v1.0/invoke/{id}/method/{method:*}


# INFO[0000] GET----/v1.0/state/{storeName}/{key}
# INFO[0000] DELETE----/v1.0/state/{storeName}/{key}
# INFO[0000] PUT----/v1.0/state/{storeName}
# INFO[0000] PUT----/v1.0/state/{storeName}/bulk
# INFO[0000] PUT----/v1.0/state/{storeName}/transaction
# INFO[0000] POST----/v1.0/state/{storeName}
# INFO[0000] POST----/v1.0/state/{storeName}/bulk
# INFO[0000] POST----/v1.0/state/{storeName}/transaction
# INFO[0000] POST----/v1.0-alpha1/state/{storeName}/query
# INFO[0000] PUT----/v1.0-alpha1/state/{storeName}/query


# INFO[0000] GET----/v1.0/secrets/{secretStoreName}/bulk
# INFO[0000] GET----/v1.0/secrets/{secretStoreName}/{key}


# INFO[0000] POST----/v1.0/publish/{pubsubname}/{topic:*}
# INFO[0000] PUT----/v1.0/publish/{pubsubname}/{topic:*}


# INFO[0000] POST----/v1.0/bindings/{name}
# INFO[0000] PUT----/v1.0/bindings/{name}


# INFO[0000] GET----/v1.0/healthz
# INFO[0000] GET----/v1.0/healthz/outbound


# INFO[0000] GET----/v1.0/actors/{actorType}/{actorId}/method/{method}
# INFO[0000] GET----/v1.0/actors/{actorType}/{actorId}/state/{key}
# INFO[0000] GET----/v1.0/actors/{actorType}/{actorId}/reminders/{name}
# INFO[0000] POST----/v1.0/actors/{actorType}/{actorId}/state
# INFO[0000] POST----/v1.0/actors/{actorType}/{actorId}/method/{method}
# INFO[0000] POST----/v1.0/actors/{actorType}/{actorId}/reminders/{name}
# INFO[0000] POST----/v1.0/actors/{actorType}/{actorId}/timers/{name}
# INFO[0000] PUT----/v1.0/actors/{actorType}/{actorId}/state
# INFO[0000] PUT----/v1.0/actors/{actorType}/{actorId}/method/{method}
# INFO[0000] PUT----/v1.0/actors/{actorType}/{actorId}/reminders/{name}
# INFO[0000] PUT----/v1.0/actors/{actorType}/{actorId}/timers/{name}
# INFO[0000] DELETE----/v1.0/actors/{actorType}/{actorId}/method/{method}
# INFO[0000] DELETE----/v1.0/actors/{actorType}/{actorId}/reminders/{name}
# INFO[0000] DELETE----/v1.0/actors/{actorType}/{actorId}/timers/{name}

# INFO[0000] *----/{method:*}

# INFO[0000] GET----/v1.0/metadata
# INFO[0000] PUT----/v1.0/metadata/{key}

# INFO[0000] POST----/v1.0/shutdown
