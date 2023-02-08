# -*- coding: utf-8 -*-
import random

from fastapi import FastAPI
from fastapi.routing import Request, Response
from starlette_exporter import PrometheusMiddleware
from starlette_exporter import handle_metrics

app = FastAPI(title="daprd")
app.add_middleware(PrometheusMiddleware)
app.add_route("/metrics", handle_metrics)


@app.get("/health")
async def get(req: Request):
    return {
        "state": "ok",
    }


@app.post("/products")
async def widgets(req: Request):
    print('products')
    return {
        "state": "ok",
    }


@app.post("/widgets")
async def widgets(req: Request):
    print('widgets')
    return {
        "state": "ok",
    }


@app.get("/dapr/config")
async def get(req: Request):
    # 没有数据传过来
    return {
        "entities": [
            "a||b",
            'actorType-a',
            'actorType-b',
            'actorType-c'  # 将决定本actor可以运行什么类型的任务
        ],
        # "actorIdleTimeout": "",  # 60m
        # "actorScanInterval": "",  # 30s
        # "drainOngoingCallTimeout": "",  # 60s
        "drainRebalancedActors": True,
        "reentrancy": {
            'enabled': True,
            'maxStackDepth': 32  # 32
        },
        "remindersStoragePartitions": 3,
    }


#  定时触发
@app.put('/actors/actorType-a/actorId-a/method/remind/demo')
async def remind(req: Request):
    print(await req.json())
    # 应用程序内的自动订阅
    return "ok"


#  定时触发
@app.put('/actors/a||b/c||d/method/remind/demo')
async def remindxx(req: Request):
    print(await req.json())
    # 应用程序内的自动订阅
    return "ok"


@app.get('/dapr/subscribe')
async def sub(req: Request):
    # print(await req.json())
    # 应用程序内的自动订阅
    return [
        {
            "pubsubname": "redis-pubsub",
            "topic": "topic-a",
            "route": "topic-a"
        },
        {
            "pubsubname": "redis-pubsub",
            "topic": "topic-b",
            "route": "topic-b"
        }
    ]


@app.post('/topic-a')
async def a(req: Request):
    print(await req.json())
    return ''


@app.post('/topic-b')
async def b(req: Request):
    print(await req.json())
    return ''


@app.post('/dsstatus')
async def sub(req: Request):
    print(await req.json())

    return {
        'status': 'SUCCESS',
    }


# dapr 会对每一个binding进行处理，判断app有没有实现对应的binding的路由，如果有,就自动进行消息处理了


@app.get('/myevent')
async def myevent(req: Request):
    if random.random() > 0.5:
        return Response("200", status_code=200)
    else:
        return Response("500", status_code=500)


@app.get('/healthz')
async def healthz():
    return 'ok'


#  input_binding
@app.post('/myevent')
async def myevent(req: Request):
    print(await req.body())
    return Response("200", status_code=200)


# 必须返回三者之一 | 不写 status
# 	Success AppResponseStatus = "SUCCESS" | ""
# 	Retry AppResponseStatus = "RETRY"
# 	Drop AppResponseStatus = "DROP"

@app.post('/post')
async def post(req: Request):
    body = await req.json()
    print(body)
    return body


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app="daprd:app", port=3001, host="0.0.0.0")
