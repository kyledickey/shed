# Mock data generator, deployed by shedlab as an ordinary shed service. It uses
# only the standard library so that it runs on the stock Python image.
import json
import os
import random
import secrets
import sys
import time
import urllib.request

KIND = os.environ["GEN_KIND"]
RATE = int(os.environ["GEN_RATE"])
SERVICE = os.environ.get("SHED_SERVICE_NAME", KIND)
OTLP = "http://localhost:4318/v1/"
BATCHES_PER_SECOND = 4
ROUTES = [
    "/",
    "/login",
    "/api/items",
    "/api/items/{id}",
    "/api/orders",
    "/healthz",
    "/search",
]
LEVELS = [("INFO", 9, 80), ("DEBUG", 5, 10), ("WARN", 13, 7), ("ERROR", 17, 3)]


def level():
    return random.choices(LEVELS, weights=[w for _, _, w in LEVELS])[0]


def attr(key, value):
    typed = (
        {"intValue": str(value)} if isinstance(value, int) else {"stringValue": value}
    )
    return {"key": key, "value": typed}


def resource(field, scope_field, items):
    return {
        field: [
            {
                "resource": {"attributes": [attr("service.name", SERVICE)]},
                scope_field: [
                    {
                        "scope": {"name": "shedlab"},
                        "logRecords"
                        if KIND == "otel-logs"
                        else "spans"
                        if KIND == "traces"
                        else "metrics": items,
                    }
                ],
            }
        ]
    }


def send(signal, body):
    req = urllib.request.Request(
        OTLP + signal, json.dumps(body).encode(), {"Content-Type": "application/json"}
    )
    try:
        urllib.request.urlopen(req, timeout=10).read()
    except Exception as err:
        print(f"send {signal}: {err}", file=sys.stderr, flush=True)
        time.sleep(1)


def stdout_logs(n):
    for _ in range(n):
        name, _, _ = level()
        print(
            json.dumps(
                {
                    "level": name,
                    "msg": "request handled",
                    "route": random.choice(ROUTES),
                    "status": random.choice([200, 200, 200, 201, 404, 500]),
                    "ms": random.randint(1, 900),
                    "trace": secrets.token_hex(8),
                }
            )
        )
    sys.stdout.flush()


def otel_logs(n):
    now = str(time.time_ns())
    records = []
    for _ in range(n):
        name, number, _ = level()
        records.append(
            {
                "timeUnixNano": now,
                "severityText": name,
                "severityNumber": number,
                "body": {"stringValue": f"handled {random.choice(ROUTES)}"},
                "attributes": [attr("user.id", random.randint(1, 5000))],
            }
        )
    send("logs", resource("resourceLogs", "scopeLogs", records))


def traces(n):
    spans = []
    while len(spans) < n:
        trace, start = (
            secrets.token_hex(16),
            time.time_ns() - random.randint(10**6, 2 * 10**9),
        )
        root, route = secrets.token_hex(8), random.choice(ROUTES)
        failed = random.random() < 0.03
        spans.append(
            {
                "traceId": trace,
                "spanId": root,
                "name": "GET " + route,
                "kind": 2,
                "startTimeUnixNano": str(start),
                "endTimeUnixNano": str(start + random.randint(10**6, 9 * 10**8)),
                "status": {"code": 2 if failed else 1},
                "attributes": [
                    attr("http.route", route),
                    attr("http.response.status_code", 500 if failed else 200),
                ],
            }
        )
        for child in ("db.query", "cache.get"):
            spans.append(
                {
                    "traceId": trace,
                    "spanId": secrets.token_hex(8),
                    "parentSpanId": root,
                    "name": child,
                    "kind": 3,
                    "startTimeUnixNano": str(start + 1000),
                    "endTimeUnixNano": str(start + random.randint(10**5, 10**8)),
                    "status": {"code": 1},
                }
            )
    send("traces", resource("resourceSpans", "scopeSpans", spans))


def metrics(n):
    now = str(time.time_ns())
    points = [
        {
            "timeUnixNano": now,
            "asDouble": random.random() * 100,
            "attributes": [
                attr("route", random.choice(ROUTES)),
                attr("instance", random.randint(1, 20)),
            ],
        }
        for _ in range(n)
    ]
    send(
        "metrics",
        resource(
            "resourceMetrics",
            "scopeMetrics",
            [
                {
                    "name": "lab.requests.latency",
                    "unit": "ms",
                    "gauge": {"dataPoints": points},
                }
            ],
        ),
    )


GENERATORS = {
    "logs": stdout_logs,
    "otel-logs": otel_logs,
    "traces": traces,
    "metrics": metrics,
}

generate = GENERATORS[KIND]
batch = max(1, RATE // BATCHES_PER_SECOND)
while True:
    began = time.monotonic()
    generate(batch)
    time.sleep(max(0, 1 / BATCHES_PER_SECOND - (time.monotonic() - began)))
